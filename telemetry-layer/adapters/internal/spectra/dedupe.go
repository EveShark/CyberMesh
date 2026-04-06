package spectra

import (
	"sync"
	"time"
)

type deduper struct {
	mu      sync.Mutex
	seen    map[string]time.Time
	ttl     time.Duration
	lastGC  time.Time
	gcEvery time.Duration
}

func newDeduper(ttl time.Duration) *deduper {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &deduper{
		seen:    make(map[string]time.Time),
		ttl:     ttl,
		gcEvery: ttl / 2,
	}
}

func (d *deduper) firstSeen(id string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gc(now)
	if at, ok := d.seen[id]; ok {
		if now.Sub(at) <= d.ttl {
			return false
		}
	}
	d.seen[id] = now
	return true
}

func (d *deduper) gc(now time.Time) {
	if d.gcEvery <= 0 {
		d.gcEvery = time.Minute
	}
	if !d.lastGC.IsZero() && now.Sub(d.lastGC) < d.gcEvery {
		return
	}
	cutoff := now.Add(-d.ttl)
	for k, at := range d.seen {
		if at.Before(cutoff) {
			delete(d.seen, k)
		}
	}
	d.lastGC = now
}
