package cache

import "time"

// evict removes one entry from the cache using S3-FIFO policy.
// Returns the evicted key, data, and whether an eviction occurred.
func (c *Cache) evict() (string, []byte, bool) {
	now := time.Now()

	// Phase 1: evict from small queue.
	for c.small.Len() > 0 {
		front := c.small.Front()
		e := front.Value.(*entry)
		c.small.Remove(front)

		if now.After(e.expiresAt) {
			c.totalWeight -= e.weight
			c.removeFromTagIndex(e.key, e.tags)
			delete(c.items, e.key)
			c.evictions.Add(1)
			return e.key, e.data, true
		}

		if e.freq > 0 {
			// Accessed while in small — promote to main.
			e.freq = 0
			e.inMain = true
			e.elem = c.main.PushBack(e)
			continue
		}

		// One-hit wonder — evict to ghost.
		key, data := e.key, e.data
		c.totalWeight -= e.weight
		c.removeFromTagIndex(e.key, e.tags)
		delete(c.items, e.key)
		c.evictions.Add(1)
		c.addToGhost(key)
		return key, data, true
	}

	// Phase 2: evict from main queue (CLOCK-like second chance).
	limit := c.main.Len()
	for i := 0; i < limit && c.main.Len() > 0; i++ {
		front := c.main.Front()
		e := front.Value.(*entry)
		c.main.Remove(front)

		if now.After(e.expiresAt) {
			c.totalWeight -= e.weight
			c.removeFromTagIndex(e.key, e.tags)
			delete(c.items, e.key)
			c.evictions.Add(1)
			return e.key, e.data, true
		}

		if e.freq > 0 {
			e.freq--
			e.elem = c.main.PushBack(e)
			continue
		}

		c.totalWeight -= e.weight
		c.removeFromTagIndex(e.key, e.tags)
		delete(c.items, e.key)
		c.evictions.Add(1)
		return e.key, e.data, true
	}

	// Safety: force evict front of main if all had freq > 0.
	if front := c.main.Front(); front != nil {
		e := front.Value.(*entry)
		c.main.Remove(front)
		c.totalWeight -= e.weight
		c.removeFromTagIndex(e.key, e.tags)
		delete(c.items, e.key)
		c.evictions.Add(1)
		return e.key, e.data, true
	}

	return "", nil, false
}

// addToGhost adds a key to the ghost queue, evicting the oldest ghost if full.
func (c *Cache) addToGhost(key string) {
	for len(c.ghostMap) >= c.ghostCap {
		front := c.ghost.Front()
		if front == nil {
			break
		}
		old := front.Value.(string)
		c.ghost.Remove(front)
		delete(c.ghostMap, old)
	}
	elem := c.ghost.PushBack(key)
	c.ghostMap[key] = elem
}

// removeEntry removes an active entry from its queue and the items map.
func (c *Cache) removeEntry(e *entry) {
	if e.inMain {
		c.main.Remove(e.elem)
	} else {
		c.small.Remove(e.elem)
	}
	c.totalWeight -= e.weight
	c.removeFromTagIndex(e.key, e.tags)
	delete(c.items, e.key)
}

// evictByWeight evicts entries until totalWeight <= MaxWeight.
// Must be called with mu held. Appends evicted entries to batch if OnEvict is set.
func (c *Cache) evictByWeight(batch *[]evictedEntry) {
	for c.totalWeight > c.cfg.MaxWeight && (c.small.Len() > 0 || c.main.Len() > 0) {
		ek, ed, ok := c.evict()
		if !ok {
			break
		}
		if c.cfg.OnEvict != nil {
			*batch = append(*batch, evictedEntry{ek, ed, EvictCapacity})
		}
	}
}

// cleanupTick removes expired entries from L1 in a single pass.
func (c *Cache) cleanupTick() {
	var batch []evictedEntry
	c.mu.Lock()
	now := time.Now()
	for key, e := range c.items {
		if now.After(e.expiresAt) {
			if c.cfg.OnEvict != nil {
				batch = append(batch, evictedEntry{key: key, data: e.data, reason: EvictExpired})
			}
			if e.inMain {
				c.main.Remove(e.elem)
			} else {
				c.small.Remove(e.elem)
			}
			c.totalWeight -= e.weight
			c.removeFromTagIndex(key, e.tags)
			delete(c.items, key)
		}
	}
	c.mu.Unlock()
	c.notifyBatch(batch)
}
