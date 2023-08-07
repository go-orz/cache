package cache

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

const ShardCount = 32

type shardedCache[K comparable, V any] struct {
	shards  []sharded[K, V]
	options []Option[K, V]

	stop chan struct{}
}

// wrapShardedCache is a wrapper around a shardedCache.
type wrapShardedCache[K comparable, V any] struct {
	*shardedCache[K, V]
}

func NewSharded[K comparable, V any](cleanupInterval time.Duration, options ...Option[K, V]) Cache[K, V] {
	c := shardedCache[K, V]{
		shards:  make([]sharded[K, V], ShardCount),
		options: options,
	}

	for i := 0; i < ShardCount; i++ {
		c.shards[i] = sharded[K, V]{
			items: make(map[K]Item[V]),
		}
	}

	cache := wrapShardedCache[K, V]{&c}

	if cleanupInterval > 0 {
		c.stop = make(chan struct{}, 1)
		var stopNightKeeper = func(c *wrapShardedCache[K, V]) {
			c.stop <- struct{}{}
		}
		go runNightKeeper2(&c, cleanupInterval)
		runtime.SetFinalizer(&cache, stopNightKeeper)
	}

	return &cache
}

// runNightKeeper is a goroutine that periodically checks the cache for expired items.
func runNightKeeper2[K comparable, V any](c *shardedCache[K, V], cleanupInterval time.Duration) {
	ticker := time.NewTicker(cleanupInterval)
	for {
		select {
		case <-ticker.C:
			c.DeleteExpired()
		case <-c.stop:
			ticker.Stop()
			for i := range c.options {
				if c.options[i].OnStopped == nil {
					continue
				}
				c.options[i].OnStopped()
			}
			return
		}
	}
}

func (r *shardedCache[K, V]) Set(k K, v V, d time.Duration) {
	s := r.getShard(k)
	s.Lock()
	defer s.Unlock()

	var e time.Time
	if d > 0 {
		e = time.Now().Add(d)
	}

	s.items[k] = Item[V]{Value: v, Expires: e}
}

func (r *shardedCache[K, V]) Add(k K, v V, d time.Duration) error {
	s := r.getShard(k)
	s.Lock()
	defer s.Unlock()

	if _, ok := s.items[k]; ok {
		return ErrKeyAlreadyExists
	}

	var e time.Time
	if d > 0 {
		e = time.Now().Add(d)
	}

	s.items[k] = Item[V]{Value: v, Expires: e}
	return nil
}

func (r *shardedCache[K, V]) Replace(k K, v V, d time.Duration) error {
	s := r.getShard(k)
	s.Lock()
	defer s.Unlock()

	if _, ok := s.items[k]; !ok {
		return ErrKeyNotExists
	}

	var e time.Time
	if d > 0 {
		e = time.Now().Add(d)
	}

	s.items[k] = Item[V]{Value: v, Expires: e}
	return nil
}

func (r *shardedCache[K, V]) Get(k K) (V, bool) {
	s := r.getShard(k)
	s.Lock()
	defer s.Unlock()

	item, ok := s.items[k]
	return item.Value, ok
}

func (r *shardedCache[K, V]) GetWithExpiration(k K) (V, time.Time, bool) {
	s := r.getShard(k)
	s.Lock()
	defer s.Unlock()

	item, ok := s.items[k]
	return item.Value, item.Expires, ok
}

func (r *shardedCache[K, V]) Keys() []K {
	var keys []K
	for i := range r.shards {
		shard := &r.shards[i]
		shard.RLock()
		for k := range shard.items {
			keys = append(keys, k)
		}
		shard.RUnlock()
	}

	return keys
}

func (r *shardedCache[K, V]) Items() map[K]Item[V] {
	items := make(map[K]Item[V])
	for i := range r.shards {
		shard := &r.shards[i]
		shard.RLock()
		for k, item := range shard.items {
			items[k] = item
		}
		shard.RUnlock()
	}

	return items
}

func (r *shardedCache[K, V]) Reset() {
	for i := range r.shards {
		shard := &r.shards[i]
		shard.Lock()
		shard.items = make(map[K]Item[V])
		shard.Unlock()
	}
}

func (r *shardedCache[K, V]) ItemCount() int {
	count := 0
	for i := range r.shards {
		shard := &r.shards[i]
		shard.RLock()
		count += len(shard.items)
		shard.RUnlock()
	}

	return count
}

func (r *shardedCache[K, V]) DeleteExpired() {
	var evictedItems []struct {
		key   K
		value V
	}

	for i := range r.shards {
		shard := &r.shards[i]
		shard.Lock()
		for k, item := range shard.items {
			if !item.Expired() {
				continue
			}
			ov, evicted := shard.delete(k)
			if evicted {
				evictedItems = append(evictedItems, struct {
					key   K
					value V
				}{k, ov})
			}
		}
		shard.Unlock()
	}

	for _, v := range evictedItems {
		options := r.options
		for i := range options {
			if options[i].OnEvicted == nil {
				continue
			}
			options[i].OnEvicted(v.key, v.value)
		}
	}
}

func (r *shardedCache[K, V]) Delete(k K) {
	shard := r.getShard(k)
	shard.Lock()
	v, evicted := shard.delete(k)
	shard.Unlock()
	if evicted {
		for i := range r.options {
			if r.options[i].OnEvicted == nil {
				continue
			}
			r.options[i].OnEvicted(k, v)
		}
	}
}

type sharded[K comparable, V any] struct {
	sync.RWMutex
	items map[K]Item[V]
}

func (r *sharded[K, V]) delete(k K) (V, bool) {
	v, found := r.items[k]
	if !found {
		var t V
		return t, false
	}
	delete(r.items, k)
	return v.Value, true
}

func (r *shardedCache[K, V]) getShard(k K) *sharded[K, V] {
	hash := fnv32a(fmt.Sprintf(`%v`, k))
	s := &r.shards[hash%ShardCount]
	return s
}

func (r *shardedCache[K, V]) Range(fn func(K, V)) {
	for idx := range r.shards {
		shard := &(r.shards)[idx]
		shard.RLock()
		for key, item := range shard.items {
			fn(key, item.Value)
		}
		shard.RUnlock()
	}
}

func fnv32a(str string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	hash := uint32(offset32)
	for i := 0; i < len(str); i++ {
		hash ^= uint32(str[i])
		hash *= prime32
	}
	return hash
}

var _ Cache[string, int] = (*shardedCache[string, int])(nil)
