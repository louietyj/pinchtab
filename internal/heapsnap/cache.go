package heapsnap

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	parse   func(path string, opts ParseOptions) (*Aggregate, error)
}

type cacheEntry struct {
	size    int64
	modTime time.Time
	agg     *Aggregate
}

func (c *Cache) Load(path string, retained bool) (*Aggregate, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat heap snapshot: %w", err)
	}
	c.mu.Lock()
	entry, ok := c.entries[path]
	parse := c.parse
	c.mu.Unlock()
	if ok && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) && (!retained || entry.agg.Retained != nil) {
		return entry.agg, nil
	}
	if parse == nil {
		parse = ParseFileWith
	}
	agg, err := parse(path, ParseOptions{Retained: retained})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]cacheEntry{}
	}
	c.entries[path] = cacheEntry{size: info.Size(), modTime: info.ModTime(), agg: agg}
	c.mu.Unlock()
	return agg, nil
}
