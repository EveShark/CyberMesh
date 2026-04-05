package controller

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

type compactionDecision struct {
	Superseded bool
	Key        string
	LastSeen   time.Time
	LastAction string
	Action     string
}

type lifecycleCompactor struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[string]compactionEntry
}

type compactionEntry struct {
	seenAt time.Time
	action string
}

func newLifecycleCompactor(window time.Duration) *lifecycleCompactor {
	if window <= 0 {
		window = 2 * time.Minute
	}
	return &lifecycleCompactor{
		window:  window,
		entries: make(map[string]compactionEntry),
	}
}

func (c *lifecycleCompactor) Observe(evt policy.Event, eventTS time.Time, now time.Time) compactionDecision {
	if c == nil {
		return compactionDecision{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if eventTS.IsZero() {
		eventTS = now
	}
	key := compactionKey(evt.Spec)
	action := normalizeCompactionAction(evt.Spec.Action)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(now)

	last, ok := c.entries[key]
	if ok && !eventTS.After(last.seenAt) {
		return compactionDecision{
			Superseded: true,
			Key:        key,
			LastSeen:   last.seenAt,
			LastAction: last.action,
			Action:     action,
		}
	}
	c.entries[key] = compactionEntry{
		seenAt: eventTS,
		action: action,
	}
	return compactionDecision{
		Superseded: false,
		Key:        key,
		LastSeen:   eventTS,
		LastAction: action,
		Action:     action,
	}
}

func (c *lifecycleCompactor) prune(now time.Time) {
	if c.window <= 0 || len(c.entries) == 0 {
		return
	}
	cutoff := now.Add(-c.window)
	for k, entry := range c.entries {
		if entry.seenAt.Before(cutoff) {
			delete(c.entries, k)
		}
	}
}

func compactionKey(spec policy.PolicySpec) string {
	id := strings.ToLower(strings.TrimSpace(spec.ID))
	if id != "" {
		return "id:" + id
	}

	// Fallback only for malformed/missing IDs.
	scope := strings.ToLower(strings.TrimSpace(scopeIdentifier(spec)))
	namespace := strings.ToLower(strings.TrimSpace(spec.Target.Namespace))
	if namespace == "" {
		namespace = strings.ToLower(strings.TrimSpace(spec.Target.Selectors["namespace"]))
	}
	node := strings.ToLower(strings.TrimSpace(spec.Target.Selectors["node"]))
	ips := append([]string(nil), spec.Target.IPs...)
	cidrs := append([]string(nil), spec.Target.CIDRs...)
	slices.Sort(ips)
	slices.Sort(cidrs)
	target := strings.Join([]string{
		strings.Join(ips, ","),
		strings.Join(cidrs, ","),
		namespace,
		node,
	}, "|")
	if target == "|||" {
		target = "unknown"
	}
	return "fallback:" + scope + "|" + target
}

func normalizeCompactionAction(action string) string {
	a := strings.ToLower(strings.TrimSpace(action))
	if a == "" {
		return "apply"
	}
	return a
}
