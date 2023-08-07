package cache

import (
	"errors"
	"runtime"
	"sync"
	"time"
)

var (
	ErrKeyAlreadyExists = errors.New("key already exists")
	ErrKeyNotExists     = errors.New("key doesn't exists")
)

const NeverExpired = 0

// Item represents a cache item that can hold any type.
type Item[V any] struct {
	Value   V
	Expires time.Time
}

// Expired checks whether the cache item has expired.
func (item Item[V]) Expired() bool {
	if item.Expires.IsZero() {
		return false
	}
	return time.Now().After(item.Expires)
}

// Cache is the interface that represents common cache functionality.
type Cache[K comparable, V any] interface {
	Set(k K, v V, d time.Duration)
	Add(k K, v V, d time.Duration) error
	Replace(k K, v V, d time.Duration) error
	Get(k K) (V, bool)
	GetWithExpiration(k K) (V, time.Time, bool)
	Keys() []K
	Items() map[K]Item[V]
	Reset()
	ItemCount() int
	DeleteExpired()
	Delete(k K)
}

// genericCache is the implementation of the Cache interface.
type genericCache[K comparable, V any] struct {
	items   map[K]Item[V]
	mu      sync.RWMutex
	options []Option[K, V]

	stop chan struct{}
}

// wrapCache is a wrapper around a genericCache.
type wrapCache[K comparable, V any] struct {
	*genericCache[K, V]
}

type (
	OnEvicted[K comparable, V any] func(K, V)
	OnStopped                      func()
)

type Option[K comparable, V any] struct {
	OnEvicted OnEvicted[K, V]
	OnStopped OnStopped
}

// New creates a new cache with a given cleanup interval and callbacks for eviction and stopping events.
func New[K comparable, V any](cleanupInterval time.Duration, options ...Option[K, V]) Cache[K, V] {
	c := genericCache[K, V]{
		items:   make(map[K]Item[V]),
		options: options,
	}

	cache := wrapCache[K, V]{&c}

	if cleanupInterval > 0 {
		c.stop = make(chan struct{}, 1)
		var stopNightKeeper = func(c *wrapCache[K, V]) {
			c.stop <- struct{}{}
		}
		go runNightKeeper(&c, cleanupInterval)
		runtime.SetFinalizer(&cache, stopNightKeeper)
	}
	return &cache
}

// runNightKeeper is a goroutine that periodically checks the cache for expired items.
func runNightKeeper[K comparable, V any](c *genericCache[K, V], cleanupInterval time.Duration) {
	ticker := time.NewTicker(cleanupInterval)
	for {
		select {
		case <-ticker.C:
			c.DeleteExpired()
		case <-c.stop:
			ticker.Stop()
			options := c.options
			for i := range options {
				if options[i].OnStopped == nil {
					continue
				}
				options[i].OnStopped()
			}
			return
		}
	}
}

// DeleteExpired removes all items that have expired from the cache.
func (r *genericCache[K, V]) DeleteExpired() {
	var evictedItems []struct {
		key   K
		value V
	}

	r.mu.Lock()
	for k, v := range r.items {
		if !v.Expired() {
			continue
		}
		ov, evicted := r.delete(k)
		if evicted {
			evictedItems = append(evictedItems, struct {
				key   K
				value V
			}{k, ov})
		}
	}
	r.mu.Unlock()

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

func (r *genericCache[K, V]) Delete(k K) {
	r.mu.Lock()
	v, evicted := r.delete(k)
	r.mu.Unlock()
	if evicted {
		for i := range r.options {
			if r.options[i].OnEvicted == nil {
				continue
			}
			r.options[i].OnEvicted(k, v)
		}
	}
}

// delete removes a cache item if eviction is enabled.
func (r *genericCache[K, V]) delete(k K) (V, bool) {
	v, found := r.items[k]
	if !found {
		var t V
		return t, false
	}
	delete(r.items, k)
	return v.Value, true
}

// Keys returns a slice of all items key within cache.
func (r *genericCache[K, V]) Keys() []K {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]K, len(r.items))
	var index = 0
	for k, v := range r.items {
		if v.Expired() {
			continue
		}
		keys[index] = k
		index++
	}
	return keys
}

// Items returns a map of all items within cache.
func (r *genericCache[K, V]) Items() map[K]Item[V] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := make(map[K]Item[V], len(r.items))
	for k, v := range r.items {
		if v.Expired() {
			continue
		}
		m[k] = v
	}
	return m
}

// Set sets a cache item assuming the cache has already been locked.
func (r *genericCache[K, V]) Set(k K, v V, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setLocked(k, v, d)
}

// setLocked sets a cache item, assuming that the cache lock has already been locked.
func (r *genericCache[K, V]) setLocked(k K, v V, d time.Duration) {
	var e time.Time
	if d > 0 {
		e = time.Now().Add(d)
	}
	r.items[k] = Item[V]{
		Value:   v,
		Expires: e,
	}
}

// Add adds a new cache item if the key does not already exist.
func (r *genericCache[K, V]) Add(k K, v V, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, found := r.get(k)
	if found {
		return ErrKeyAlreadyExists
	}
	r.setLocked(k, v, d)

	return nil
}

// Replace replaces a cache item if the key already exists.
func (r *genericCache[K, V]) Replace(k K, v V, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, found := r.get(k)
	if !found {
		return ErrKeyNotExists
	}
	r.setLocked(k, v, d)
	return nil
}

// Get returns a cache item by key.
func (r *genericCache[K, V]) Get(k K) (V, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, found := r.items[k]
	if !found || item.Expired() {
		var t V
		return t, false
	}
	return item.Value, true
}

// GetWithExpiration returns a cache item and its expiration time by key.
func (r *genericCache[K, V]) GetWithExpiration(k K) (V, time.Time, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, found := r.items[k]
	if !found || item.Expired() {
		var t V
		return t, time.Time{}, false
	}
	return item.Value, item.Expires, true
}

// get returns a Value from the cache.
func (r *genericCache[K, V]) get(k K) (V, bool) {
	item, found := r.items[k]
	if !found || item.Expired() {
		var t V
		return t, false
	}
	return item.Value, true
}

// ItemCount counts the number of items in the cache.
func (r *genericCache[K, V]) ItemCount() int {
	r.mu.RLock()
	n := len(r.items)
	r.mu.RUnlock()
	return n
}

// Reset removes all items from the cache.
func (r *genericCache[K, V]) Reset() {
	r.mu.Lock()
	r.items = map[K]Item[V]{}
	r.mu.Unlock()
}

// _ is an assertion that genericCache implements Cache.
var _ Cache[string, int] = (*genericCache[string, int])(nil)
