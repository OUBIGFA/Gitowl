package gitcli

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"sync"
)

type cacheEntry struct {
	key  string
	data []byte
	cost int64
}
type cache struct {
	mu          sync.Mutex
	limit, used int64
	items       map[string]*list.Element
	lru         *list.List
}

func newCache(limit int64) *cache {
	return &cache{limit: limit, items: make(map[string]*list.Element), lru: list.New()}
}
func (c *cache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.items[key]
	if e == nil {
		return nil, false
	}
	c.lru.MoveToFront(e)
	return bytes.Clone(e.Value.(cacheEntry).data), true
}
func (c *cache) put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Charge keys and a conservative per-entry overhead as well as payloads.
	cost := int64(len(key) + len(data) + 128)
	if cost > c.limit || len(data) > maxCachedObject {
		return
	}
	if old := c.items[key]; old != nil {
		c.used -= old.Value.(cacheEntry).cost
		c.lru.Remove(old)
		delete(c.items, key)
	}
	for c.used+cost > c.limit {
		old := c.lru.Back()
		entry := old.Value.(cacheEntry)
		c.used -= entry.cost
		delete(c.items, entry.key)
		c.lru.Remove(old)
	}
	c.items[key] = c.lru.PushFront(cacheEntry{key, bytes.Clone(data), cost})
	c.used += cost
}

func (e *Engine) load(ctx context.Context, key string, read func() ([]byte, error)) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if data, ok := e.cache.get(key); ok {
			e.hits.Add(1)
			return data, nil
		}
		e.mu.Lock()
		// A load may finish between the first cache check and acquiring this
		// lock. Recheck before registering another process for the same object.
		if data, ok := e.cache.get(key); ok {
			e.mu.Unlock()
			e.hits.Add(1)
			return data, nil
		}
		if flight := e.loads[key]; flight != nil {
			e.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-flight.done:
			}
			// A shared task belongs to its initiating request. Its cancellation
			// or deadline must not cancel another caller that still needs data.
			if (errors.Is(flight.err, context.Canceled) || errors.Is(flight.err, context.DeadlineExceeded)) && ctx.Err() == nil {
				continue
			}
			return bytes.Clone(flight.data), flight.err
		}
		flight := &objectLoad{done: make(chan struct{})}
		e.loads[key] = flight
		e.mu.Unlock()
		flight.data, flight.err = read()
		if flight.err == nil {
			e.cache.put(key, flight.data)
		}
		e.mu.Lock()
		delete(e.loads, key)
		close(flight.done)
		e.mu.Unlock()
		return bytes.Clone(flight.data), flight.err
	}
}
func (e *Engine) Stats() Stats {
	e.cache.mu.Lock()
	defer e.cache.mu.Unlock()
	return Stats{e.commands.Load(), e.hits.Load(), e.cache.used, len(e.cache.items)}
}
