package auth

import (
	"sync"
	"time"
)

type cachedDecision struct {
	outcome   validationOutcome
	expiresAt time.Time
}

// decisionCache stores short lived validation outcomes to reduce Firestore traffic.
type decisionCache struct {
	mu   sync.RWMutex
	data map[string]cachedDecision
}

func newDecisionCache() *decisionCache {
	return &decisionCache{
		data: make(map[string]cachedDecision),
	}
}

func (c *decisionCache) Get(key string, now time.Time) (validationOutcome, bool) {
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		if ok {
			c.Delete(key)
		}
		return "", false
	}
	return entry.outcome, true
}

func (c *decisionCache) Set(key string, outcome validationOutcome, ttl time.Duration, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = cachedDecision{
		outcome:   outcome,
		expiresAt: now.Add(ttl),
	}
}

func (c *decisionCache) Delete(key string) {
	c.mu.Lock()
	delete(c.data, key)
	c.mu.Unlock()
}
