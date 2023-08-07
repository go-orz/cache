package cache

import (
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestSharded(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 100)

	c.Set("key1", 1, time.Millisecond*200)
	val, exists := c.Get("key1")

	if !exists || val != 1 {
		t.Errorf("Failed to get key1")
	}

	_ = c.Add("key2", 2, time.Millisecond*200)

	if c.ItemCount() != 2 {
		t.Errorf("Failed to add key2")
	}

	err := c.Add("key1", 3, time.Millisecond*200)
	if err == nil {
		t.Errorf("Failed to check existing key")
	}

	val, _ = c.Get("key2")
	if val != 2 {
		t.Errorf("Failed to get key2")
	}

	time.Sleep(time.Millisecond * 210)

	val, exists = c.Get("key1")
	if exists {
		t.Errorf("Failed to cleanup key1")
	}

	c.Reset()
	if c.ItemCount() != 0 {
		t.Errorf("Failed to reset cache")
	}
}
func TestShardedSet(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)

	c.Set("key1", 1, 0)
	val, exists := c.Get("key1")
	if !exists || val != 1 {
		t.Errorf("Failed to set key1")
	}
}

func TestShardedReplace(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)

	err := c.Replace("key1", 1, 0)
	if err == nil {
		t.Errorf("Replace should have failed")
	}

	c.Set("key1", 1, 0)
	err = c.Replace("key1", 2, 0)

	if err != nil {
		t.Errorf("Replace failed")
	}

	val, _ := c.Get("key1")
	if val != 2 {
		t.Errorf("Failed to replace key1 Value")
	}
}

func TestShardedItems(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)

	for i := 0; i < 10; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	items := c.Items()

	for k, v := range items {
		expectVal := int([]rune(k)[0] - '0')
		if v.Value != expectVal {
			t.Errorf("Items failed")
		}
	}
}

func TestShardedItemCount(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)

	for i := 0; i < 10; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	if c.ItemCount() != 10 {
		t.Errorf("ItemCount failed")
	}
}

func TestShardedGetWithExpiration(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)

	c.Set("key1", 1, time.Millisecond*100)
	_, expiration, _ := c.GetWithExpiration("key1")

	if time.Now().After(expiration) {
		t.Errorf("Expires time should be in the future")
	}
}

func TestShardedDeleteExpired(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)

	c.Set("key1", 1, time.Millisecond*100)
	time.Sleep(time.Millisecond * 210)
	c.DeleteExpired()
	_, exists := c.Get("key1")
	if exists {
		t.Errorf("Failed to delete expired")
	}
}

func TestShardedDelete(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)

	c.Set("key1", 1, time.Second*100)
	c.Delete("key1")
	_, exists := c.Get("key1")
	if exists {
		t.Errorf("Failed to delete expired")
	}
}

func TestShardedOnEvicted(t *testing.T) {
	var (
		key string
		val int
		mu  sync.Mutex
	)
	c := NewSharded[string, int](time.Millisecond*100, Option[string, int]{
		OnEvicted: func(s string, i int) {
			mu.Lock()
			key = s
			val = i
			mu.Unlock()
		},
	})

	var (
		expectKey   = "key1"
		expectValue = 1
	)
	c.Set(expectKey, expectValue, time.Millisecond*100)

	time.Sleep(time.Millisecond * 500)

	mu.Lock()
	if key != expectKey {
		t.Errorf(`expect get evicted key: %s, but got: %s`, expectKey, key)
	}
	if val != expectValue {
		t.Errorf(`expect get evicted val: %d, but got: %d`, expectValue, val)
	}
	mu.Unlock()
}

func TestShardedNeverExpired(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)
	c.Set("key1", 1, NeverExpired)

	_, exists := c.Get("key1")
	if !exists {
		t.Errorf("Failed to set never expired key")
	}
}

func TestShardedOnStopped(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(1)

	c := NewSharded[string, int](time.Millisecond*200, Option[string, int]{
		OnStopped: func() {
			wg.Done()
		},
	})

	c.Set("key1", 1, time.Millisecond*100)

	c = nil
	runtime.GC()
	wg.Wait()
}

func TestShardedKeys(t *testing.T) {
	c := NewSharded[string, int](time.Millisecond * 200)
	c.Set("key1", 1, NeverExpired)
	c.Set("key2", 1, time.Millisecond*100)

	time.Sleep(time.Millisecond * 200)

	keys := c.Keys()
	if len(keys) != 1 {
		t.Errorf("expect keys lenth is 1, but got %d", len(keys))
		return
	}

	if keys[0] != "key1" {
		t.Errorf("expect key is key1, but got %s", keys[0])
	}
}
