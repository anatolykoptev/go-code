package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Get retrieves a value from L1 (then L2 if configured). Returns nil, false on miss.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool) {
	c.mu.Lock()

	e, ok := c.items[key]
	if ok && !time.Now().After(e.expiresAt) {
		// IdleTTL check must happen BEFORE updating lastAccess.
		if c.cfg.IdleTTL > 0 && time.Since(e.lastAccess) > c.cfg.IdleTTL {
			expKey, expData := e.key, e.data
			c.removeEntry(e)
			c.mu.Unlock()
			c.notifyEvict(expKey, expData, EvictExpired)
			c.misses.Add(1)
			return nil, false
		}
		if e.freq < maxFreq {
			e.freq++
		}
		if c.cfg.IdleTTL > 0 {
			e.lastAccess = time.Now()
		}
		data := e.data
		c.mu.Unlock()
		c.hits.Add(1)
		return data, true
	}

	// L1 miss or expired.
	var expKey string
	var expData []byte
	wasExpired := ok
	if ok {
		expKey, expData = e.key, e.data
		c.removeEntry(e)
	}
	c.mu.Unlock()
	if wasExpired {
		c.notifyEvict(expKey, expData, EvictExpired)
	}

	// Try L2.
	if c.l2 != nil {
		data, err := c.l2.Get(ctx, key)
		if err == nil {
			c.l2hits.Add(1)
			c.Set(ctx, key, data)
			return data, true
		}
		if errors.Is(err, ErrCacheMiss) {
			c.l2misses.Add(1)
		} else {
			c.l2errors.Add(1)
		}
	}

	c.misses.Add(1)
	return nil, false
}

// Set stores a value in L1 (and L2 if configured) using global TTLs.
func (c *Cache) Set(ctx context.Context, key string, data []byte) {
	c.setInternal(ctx, key, data, c.cfg.L1TTL, c.cfg.L2TTL)
}

// SetWithTTL stores a value with a custom TTL for both L1 and L2.
// L1 applies jitter to the provided TTL; L2 uses it directly.
// If ttl <= 0, falls back to global TTLs (same as Set).
func (c *Cache) SetWithTTL(ctx context.Context, key string, data []byte, ttl time.Duration) {
	if ttl <= 0 {
		c.Set(ctx, key, data)
		return
	}
	c.setInternal(ctx, key, data, ttl, ttl)
}

func (c *Cache) setInternal(ctx context.Context, key string, data []byte, l1TTL, l2TTL time.Duration, tags ...string) {
	c.mu.Lock()

	// Update existing entry.
	if e, ok := c.items[key]; ok {
		if c.cfg.Weigher != nil {
			newW := c.cfg.Weigher(key, data)
			c.totalWeight += newW - e.weight
			e.weight = newW
		}
		e.data = data
		e.expiresAt = time.Now().Add(c.jitteredTTL(l1TTL))
		if len(tags) > 0 {
			c.updateTags(key, e.tags, tags)
			e.tags = tags
		}
		c.mu.Unlock()
		// Write-through to L2 (best-effort).
		if c.l2 != nil {
			if err := c.l2.Set(ctx, key, data, l2TTL); err != nil {
				slog.Warn("cache: L2 set failed", slog.String("key", key), slog.Any("error", err))
				c.l2errors.Add(1)
			}
		}
		return
	}

	// Evict until under capacity — collect evicted entries for callback.
	var batch []evictedEntry
	for len(c.items) >= c.cfg.L1MaxItems {
		ek, ed, ok := c.evict()
		if !ok {
			break
		}
		if c.cfg.OnEvict != nil {
			batch = append(batch, evictedEntry{ek, ed, EvictCapacity})
		}
	}

	// Check ghost for frequency boost.
	var initFreq uint8
	if ge, ok := c.ghostMap[key]; ok {
		c.ghost.Remove(ge)
		delete(c.ghostMap, key)
		initFreq = 1 // ghost re-admission boost
	}

	// Insert into small queue.
	now := time.Now()
	e := &entry{
		key:        key,
		data:       data,
		expiresAt:  now.Add(c.jitteredTTL(l1TTL)),
		freq:       initFreq,
		tags:       tags,
		lastAccess: now,
	}
	if c.cfg.Weigher != nil {
		e.weight = c.cfg.Weigher(key, data)
		c.totalWeight += e.weight
		if c.cfg.MaxWeight > 0 && c.totalWeight > c.cfg.MaxWeight {
			c.evictByWeight(&batch)
		}
	}
	e.elem = c.small.PushBack(e)
	c.items[key] = e
	if len(tags) > 0 {
		c.addToTagIndex(key, tags)
	}
	c.mu.Unlock()
	c.notifyBatch(batch)

	// Write-through to L2 (best-effort).
	if c.l2 != nil {
		if err := c.l2.Set(ctx, key, data, l2TTL); err != nil {
			slog.Warn("cache: L2 set failed", slog.String("key", key), slog.Any("error", err))
			c.l2errors.Add(1)
		}
	}
}

// Delete removes a key from both L1 and L2.
func (c *Cache) Delete(ctx context.Context, key string) {
	var delKey string
	var delData []byte
	var found bool
	c.mu.Lock()
	if e, ok := c.items[key]; ok {
		delKey, delData = e.key, e.data
		found = true
		c.removeEntry(e)
	}
	c.mu.Unlock()
	if found {
		c.notifyEvict(delKey, delData, EvictExplicit)
	}

	// Delete from L2 (best-effort).
	if c.l2 != nil {
		if err := c.l2.Del(ctx, key); err != nil {
			slog.Warn("cache: L2 del failed", slog.String("key", key), slog.Any("error", err))
			c.l2errors.Add(1)
		}
	}
}
