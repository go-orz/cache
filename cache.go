package cache

import (
	"errors"
	"runtime"
	"sync"
	"time"
)

var (
	ErrKeyAlreadyExists = errors.New("key already exists")
	ErrKeyNotExists     = errors.New("key doesn't exist")
)

// Item represents a cache item that can hold any type.
type Item[T any] struct {
	Value      T
	Expiration time.Time
}

// Expired checks whether the cache item has expired.
func (item Item[T]) Expired() bool {
	if item.Expiration.IsZero() {
		return false
	}
	return time.Now().After(item.Expiration)
}

// Cache is the interface that represents common cache functionality.
type Cache[T any] interface {
	Set(k string, v T, d time.Duration)
	Add(k string, v T, d time.Duration) error
	Replace(k string, v T, d time.Duration) error
	Get(k string) (T, bool)
	GetWithExpiration(k string) (T, time.Time, bool)
	Items() map[string]Item[T]
	Reset()
	ItemCount() int
	DeleteExpired()
}

// genericCache is the implementation of the Cache interface.
type genericCache[T any] struct {
	items   map[string]Item[T]
	mu      sync.RWMutex
	options []Option[T]

	cleanupInterval time.Duration
	stop            chan struct{}
}

// wrapCache is a wrapper around a genericCache.
type wrapCache[T any] struct {
	*genericCache[T]
}

type (
	OnEvicted[T any] func(string, T)
	OnStopped        func()
)

type Option[T any] struct {
	OnEvicted OnEvicted[T]
	OnStopped OnStopped
}

// New creates a new cache with a given cleanup interval and callbacks for eviction and stopping events.
func New[T any](cleanupInterval time.Duration, options ...Option[T]) Cache[T] {
	c := genericCache[T]{
		items:           make(map[string]Item[T]),
		cleanupInterval: cleanupInterval,
		options:         options,
	}

	cache := wrapCache[T]{&c}

	if cleanupInterval > 0 {
		c.stop = make(chan struct{}, 1)
		var stopNightKeeper = func(c *wrapCache[T]) {
			c.stop <- struct{}{}
		}
		go runNightKeeper(&c, cleanupInterval)
		runtime.SetFinalizer(&cache, stopNightKeeper)
	}
	return &cache
}

// runNightKeeper is a goroutine that periodically checks the cache for expired items.
func runNightKeeper[T any](c *genericCache[T], cleanupInterval time.Duration) {
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

// DeleteExpired removes all items that have expired from the cache.
func (r *genericCache[T]) DeleteExpired() {
	var evictedItems []struct {
		key   string
		value T
	}

	r.mu.Lock()
	for k, v := range r.items {
		if !v.Expired() {
			continue
		}
		ov, evicted := r.delete(k)
		if evicted {
			evictedItems = append(evictedItems, struct {
				key   string
				value T
			}{k, ov})
		}
	}
	r.mu.Unlock()

	for _, v := range evictedItems {
		for i := range r.options {
			if r.options[i].OnEvicted == nil {
				continue
			}
			r.options[i].OnEvicted(v.key, v.value)
		}
	}
}

// delete removes a cache item if eviction is enabled.
func (r *genericCache[T]) delete(k string) (T, bool) {
	v, found := r.items[k]
	if !found {
		var t T
		return t, false
	}
	delete(r.items, k)
	return v.Value, true
}

// Items returns a map of all items within cache.
func (r *genericCache[T]) Items() map[string]Item[T] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := make(map[string]Item[T], len(r.items))
	for k, v := range r.items {
		if v.Expired() {
			continue
		}
		m[k] = v
	}
	return m
}

// Set sets a cache item assuming the cache has already been locked.
func (r *genericCache[T]) Set(k string, v T, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setLocked(k, v, d)
}

// setLocked sets a cache item, assuming that the cache lock has already been locked.
func (r *genericCache[T]) setLocked(k string, v T, d time.Duration) {
	var e time.Time
	if d > 0 {
		e = time.Now().Add(d)
	}
	r.items[k] = Item[T]{
		Value:      v,
		Expiration: e,
	}
}

// Add adds a new cache item if the key does not already exist.
func (r *genericCache[T]) Add(k string, v T, d time.Duration) error {
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
func (r *genericCache[T]) Replace(k string, v T, d time.Duration) error {
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
func (r *genericCache[T]) Get(k string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, found := r.items[k]
	if !found || item.Expired() {
		var t T
		return t, false
	}
	return item.Value, true
}

// GetWithExpiration returns a cache item and its expiration time by key.
func (r *genericCache[T]) GetWithExpiration(k string) (T, time.Time, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, found := r.items[k]
	if !found || item.Expired() {
		var t T
		return t, time.Time{}, false
	}
	return item.Value, item.Expiration, true
}

// get returns a value from the cache.
func (r *genericCache[T]) get(k string) (T, bool) {
	item, found := r.items[k]
	if !found || item.Expired() {
		var t T
		return t, false
	}
	return item.Value, true
}

// ItemCount counts the number of items in the cache.
func (r *genericCache[T]) ItemCount() int {
	r.mu.RLock()
	n := len(r.items)
	r.mu.RUnlock()
	return n
}

// Reset removes all items from the cache.
func (r *genericCache[T]) Reset() {
	r.mu.Lock()
	r.items = map[string]Item[T]{}
	r.mu.Unlock()
}

// _ is an assertion that genericCache implements Cache.
var _ Cache[string] = &(genericCache[string]{})
