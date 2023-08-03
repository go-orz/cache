# Generic In-Memory Cache in Go

This repository contains an implementation of an in-memory cache in Go, using the new Go generics feature introduced in Go 1.18.

## Features

- Using Go 1.18 generics.
- Thread-safe.
- Auto cleanup of expired items.
- Supports setting a cache item with a defined expiration duration.
- Replace, Add, and Get cache items.

## Usage

```go
package main

import (
	"time"

	"github.com/go-orz/cache"
)

func main() {
	c := cache.New[string,string](time.Minute)

	c.Set("key", "value", 5*time.Minute)
	v, found := c.Get("key")
	if found {
		println(v)
	}
}

```

## Concurrency

The cache package has built-in concurrency safety. It employs mutual exclusion locks (via sync.RWMutex) to assure multiple goroutines can operate on the data structure safely.

## Error Handling

The package defines two error variables that methods can return:
 - ErrKeyAlreadyExists: Returned from the Add method if an attempt is made to add an item with a key that already exists in the cache.
- ErrKeyNotExists: Returned from the Replace method if an attempt is made to replace an item with a key that doesn't exist.

