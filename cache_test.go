package cache

import (
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

type TestStruct struct {
	Num      int
	Children []*TestStruct
}

func TestCache(t *testing.T) {
	c := New[int](time.Millisecond * 100)

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
func TestSet(t *testing.T) {
	c := New[int](time.Millisecond * 200)

	c.Set("key1", 1, 0)
	val, exists := c.Get("key1")
	if !exists || val != 1 {
		t.Errorf("Failed to set key1")
	}
}

func TestReplace(t *testing.T) {
	c := New[int](time.Millisecond * 200)

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
		t.Errorf("Failed to replace key1 value")
	}
}

func TestItems(t *testing.T) {
	c := New[int](time.Millisecond * 200)

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

func TestItemCount(t *testing.T) {
	c := New[int](time.Millisecond * 200)

	for i := 0; i < 10; i++ {
		c.Set(strconv.Itoa(i), i, 0)
	}

	if c.ItemCount() != 10 {
		t.Errorf("ItemCount failed")
	}
}

func TestGetWithExpiration(t *testing.T) {
	c := New[int](time.Millisecond * 200)

	c.Set("key1", 1, time.Millisecond*100)
	_, expiration, _ := c.GetWithExpiration("key1")

	if time.Now().After(expiration) {
		t.Errorf("Expiration time should be in the future")
	}
}

func TestDeleteExpired(t *testing.T) {
	c := New[int](time.Millisecond * 200)

	c.Set("key1", 1, time.Millisecond*100)
	time.Sleep(time.Millisecond * 210)
	c.DeleteExpired()
	_, exists := c.Get("key1")
	if exists {
		t.Errorf("Failed to delete expired")
	}
}

func TestDelete(t *testing.T) {
	c := New[int](time.Millisecond * 200)

	c.Set("key1", 1, time.Second*100)
	c.Delete("key1")
	_, exists := c.Get("key1")
	if exists {
		t.Errorf("Failed to delete expired")
	}
}

func TestOnEvicted(t *testing.T) {
	var (
		key string
		val int
		mu  sync.Mutex
	)
	c := New[int](time.Millisecond*100, Option[int]{
		OnEvicted: func(s string, i int) {
			mu.Lock()
			key = s
			val = i
			t.Log(`evicted`, s, i)
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

func TestNeverExpired(t *testing.T) {
	c := New[int](time.Millisecond * 200)
	c.Set("key1", 1, NeverExpired)

	_, exists := c.Get("key1")
	if !exists {
		t.Errorf("Failed to set never expired key")
	}
}

func TestOnStopped(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(1)

	c := New[int](time.Millisecond*200, Option[int]{
		OnStopped: func() {
			wg.Done()
		},
	})

	c.Set("key1", 1, time.Millisecond*100)

	c = nil
	runtime.GC()
	wg.Wait()
}

func TestKeys(t *testing.T) {
	c := New[int](time.Millisecond * 200)
	c.Set("key1", 1, NeverExpired)
	c.Set("key2", 1, time.Millisecond*100)

	time.Sleep(time.Millisecond * 200)

	keys := c.Keys()
	if len(keys) != 1 {
		t.Errorf("expect keys lenth is 1, but got %d", len(keys))
	}

	if keys[0] != "key1" {
		t.Errorf("expect key is key1, but got %s", keys[0])
	}
}

// BenchmarkSet tests the performance of Set method.
func BenchmarkSet(b *testing.B) {
	cache := New[string](time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set("key", "value", time.Minute)
	}
}

// BenchmarkGet tests the performance of Get method.
func BenchmarkGet(b *testing.B) {
	cache := New[string](time.Minute)
	cache.Set("key", "value", time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("key")
	}
}

// BenchmarkDeleteExpired tests the performance of DeleteExpired method.
func BenchmarkDeleteExpired(b *testing.B) {
	cache := New[string](time.Minute)
	for i := 0; i < b.N; i++ {
		cache.Set("key", "value", time.Minute)
	}
	b.ResetTimer()

	cache.DeleteExpired()
}

// BenchmarkReset tests the performance of Reset method.
func BenchmarkReset(b *testing.B) {
	cache := New[string](time.Minute)
	for i := 0; i < b.N; i++ {
		cache.Set("key", "value", time.Minute)
	}
	b.ResetTimer()

	cache.Reset()
}

// BenchmarkAdd tests the performance of Add method.
func BenchmarkAdd(b *testing.B) {
	cache := New[string](time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := cache.Add(fmt.Sprintf("key-%d", i), "value", time.Minute)
		if err != nil {
			b.Error(err)
		}
	}
}

// BenchmarkReplace tests the performance of Replace method.
func BenchmarkReplace(b *testing.B) {
	cache := New[string](time.Minute)
	cache.Set("key", "value", time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := cache.Replace("key", "value", time.Minute)
		if err != nil {
			b.Error(err)
		}
	}
}
