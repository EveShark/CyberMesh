package mempool

import (
	"container/heap"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"backend/pkg/control/policytrace"
	"backend/pkg/state"
	"backend/pkg/utils"
)

var (
	ErrDuplicate         = errors.New("duplicate transaction")
	ErrRateLimited       = errors.New("producer rate limited")
	ErrMempoolFull       = errors.New("mempool at capacity")
	ErrPriorityClassFull = errors.New("mempool priority class at capacity")
	ErrInvalidTx         = errors.New("invalid transaction")
)

type Config struct {
	MaxTxs        int
	MaxBytes      int
	MaxTxsP0      int
	MaxTxsP1      int
	MaxTxsP2      int
	NonceTTL      time.Duration
	Skew          time.Duration
	RatePerSecond int
}

type entry struct {
	Tx            state.Transaction
	Hash          [32]byte
	ProducerID    []byte
	Nonce         []byte
	Severity      uint8
	Confidence    float64
	PriorityClass string
	Ts            int64
	Size          int
}

type ErrClassFull struct {
	Class string
}

func (e ErrClassFull) Error() string {
	className := normalizePriorityClass(e.Class)
	return "mempool priority class " + className + " at capacity"
}

func (e ErrClassFull) Is(target error) bool {
	return target == ErrPriorityClassFull
}

// ProducerNonce identifies a tx by producer_id + nonce envelope identity.
type ProducerNonce struct {
	ProducerID []byte
	Nonce      []byte
}

// priority ordering: higher severity, higher confidence, earlier ts, lexicographic hash
func lessPriority(a, b *entry) bool {
	if classRank(a.PriorityClass) != classRank(b.PriorityClass) {
		return classRank(a.PriorityClass) > classRank(b.PriorityClass)
	}
	if a.Severity != b.Severity {
		return a.Severity > b.Severity
	}
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	if a.Ts != b.Ts {
		return a.Ts < b.Ts
	}
	return hex.EncodeToString(a.Hash[:]) < hex.EncodeToString(b.Hash[:])
}

// minHeap keeps lowest-priority element at top for eviction decisions
type minHeap []*entry

func (h minHeap) Len() int            { return len(h) }
func (h minHeap) Less(i, j int) bool  { return lessPriority(h[j], h[i]) } // reversed for min-heap
func (h minHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(*entry)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

type bucket struct {
	capacity int
	tokens   float64
	refill   float64 // tokens per second
	last     time.Time
}

func (b *bucket) allow(now time.Time) bool {
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.refill
	if b.tokens > float64(b.capacity) {
		b.tokens = float64(b.capacity)
	}
	b.last = now
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

type producerState struct {
	bucket bucket
	nonces map[string]time.Time // hex(nonce) -> expiry
}

type Mempool struct {
	mu          sync.RWMutex
	log         *utils.Logger
	trace       *policytrace.Collector
	cfg         Config
	entries     map[string]*entry         // hexdigest(hash) -> entry
	total       int                       // bytes
	evictHeap   minHeap                   // eviction heap (lowest priority at root)
	prod        map[string]*producerState // hex(producerID) -> state
	classCounts map[string]int
}

func (m *Mempool) ProducerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.prod)
}

// HasPolicyTx returns true when at least one policy transaction is pending.
func (m *Mempool) HasPolicyTx() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.entries {
		if e != nil && e.Tx != nil && e.Tx.Type() == state.TxPolicy {
			return true
		}
	}
	return false
}

func extractPolicyStageIdentityFromPayload(payload []byte) (string, string) {
	if len(payload) == 0 {
		return "", ""
	}
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return "", ""
	}
	get := func(m map[string]any, key string) string {
		if m == nil {
			return ""
		}
		if s, ok := m[key].(string); ok {
			return s
		}
		return ""
	}
	policyID := get(root, "policy_id")
	traceID := get(root, "trace_id")
	if metadata, ok := root["metadata"].(map[string]any); ok && traceID == "" {
		traceID = get(metadata, "trace_id")
	}
	if trace, ok := root["trace"].(map[string]any); ok && traceID == "" {
		traceID = get(trace, "id")
	}
	if nested, ok := root["params"].(map[string]any); ok {
		if policyID == "" {
			policyID = get(nested, "policy_id")
		}
		if traceID == "" {
			traceID = get(nested, "trace_id")
		}
		if metadata, ok := nested["metadata"].(map[string]any); ok && traceID == "" {
			traceID = get(metadata, "trace_id")
		}
		if trace, ok := nested["trace"].(map[string]any); ok && traceID == "" {
			traceID = get(trace, "id")
		}
	}
	return policyID, traceID
}

func New(cfg Config, log *utils.Logger) *Mempool {
	h := make(minHeap, 0)
	heap.Init(&h)
	return &Mempool{
		log:       log,
		cfg:       cfg,
		entries:   make(map[string]*entry),
		evictHeap: h,
		prod:      make(map[string]*producerState),
		classCounts: map[string]int{
			"p0": 0,
			"p1": 0,
			"p2": 0,
		},
	}
}

func (m *Mempool) SetTraceCollector(trace *policytrace.Collector) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.trace = trace
	m.mu.Unlock()
}

// cleanup producer nonce entries that expired
func (m *Mempool) cleanupNonces(now time.Time, ps *producerState) {
	for k, exp := range ps.nonces {
		if now.After(exp) {
			delete(ps.nonces, k)
		}
	}
}

type AdmissionMeta struct {
	Severity      uint8
	Confidence    float64
	PriorityClass string
}

// Add admits a transaction with strict checks and bounded eviction
func (m *Mempool) Add(tx state.Transaction, meta AdmissionMeta, now time.Time) error {
	if tx == nil {
		return ErrInvalidTx
	}
	env := tx.Envelope()
	if env == nil {
		return ErrInvalidTx
	}
	// Validate basic invariants
	if err := tx.Validate(now, m.cfg.Skew); err != nil {
		return err
	}
	// Resolve hash and identifiers
	var h [32]byte
	h = env.ContentHash
	key := hex.EncodeToString(h[:])
	pid := hex.EncodeToString(env.ProducerID)
	nonce := hex.EncodeToString(env.Nonce)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.entries[key]; exists {
		return ErrDuplicate
	}

	// Producer state and nonce replay check (defense-in-depth)
	ps, ok := m.prod[pid]
	if !ok {
		ps = &producerState{
			bucket: bucket{capacity: m.cfg.RatePerSecond, tokens: float64(m.cfg.RatePerSecond), refill: float64(m.cfg.RatePerSecond), last: now},
			nonces: make(map[string]time.Time),
		}
		m.prod[pid] = ps
	}
	m.cleanupNonces(now, ps)
	if _, used := ps.nonces[nonce]; used {
		return ErrInvalidTx
	}
	if tx.Type() == state.TxPolicy {
		policyID, traceID := extractPolicyStageIdentityFromPayload(tx.Payload())
		if policyID != "" {
			if m.log != nil {
				m.log.Info("policy stage marker",
					utils.ZapString("stage", "t_nonce_idempotency_ok"),
					utils.ZapString("policy_id", policyID),
					utils.ZapString("trace_id", traceID),
					utils.ZapInt64("t_ms", now.UnixMilli()))
			}
			if m.trace != nil {
				m.trace.Record(policytrace.Marker{
					Stage:       "t_nonce_idempotency_ok",
					PolicyID:    policyID,
					TraceID:     traceID,
					TimestampMs: now.UnixMilli(),
				})
			}
		}
	}
	// Rate limiting
	if m.cfg.RatePerSecond > 0 && !ps.bucket.allow(now) {
		return ErrRateLimited
	}

	e := &entry{
		Tx:            tx,
		Hash:          h,
		ProducerID:    env.ProducerID,
		Nonce:         env.Nonce,
		Severity:      meta.Severity,
		Confidence:    meta.Confidence,
		PriorityClass: normalizePriorityClass(meta.PriorityClass),
		Ts:            tx.Timestamp(),
		Size:          len(tx.Payload()),
	}
	if classCap := m.classCap(e.PriorityClass); classCap > 0 && m.classCounts[e.PriorityClass] >= classCap {
		victimKey, victim := m.weakestInClassLocked(e.PriorityClass)
		if victim == nil || !lessPriority(e, victim) {
			return ErrClassFull{Class: e.PriorityClass}
		}
		delete(m.entries, victimKey)
		m.total -= victim.Size
		m.decClassCount(victim.PriorityClass)
		// Replacement-heavy workloads can accumulate stale heap nodes when we
		// delete directly from entries. Rebuild now to keep eviction costs stable.
		m.rebuildEvictHeapLocked()
	}

	// Ensure capacity by evicting lowest priority as needed
	needBytes := m.total + e.Size - m.cfg.MaxBytes
	needCount := len(m.entries) + 1 - m.cfg.MaxTxs

	for (needBytes > 0 || needCount > 0) && m.evictHeap.Len() > 0 {
		victim := m.evictHeap[0]
		vkey := hex.EncodeToString(victim.Hash[:])
		if _, ok := m.entries[vkey]; !ok {
			heap.Pop(&m.evictHeap)
			continue
		}
		if !lessPriority(e, victim) {
			return ErrMempoolFull
		}

		heap.Pop(&m.evictHeap)
		delete(m.entries, vkey)
		m.total -= victim.Size
		m.decClassCount(victim.PriorityClass)
		needBytes = m.total + e.Size - m.cfg.MaxBytes
		needCount = len(m.entries) + 1 - m.cfg.MaxTxs
	}

	// if still cannot add, reject
	if m.cfg.MaxTxs > 0 && len(m.entries) >= m.cfg.MaxTxs {
		return ErrMempoolFull
	}
	if m.cfg.MaxBytes > 0 && m.total+e.Size > m.cfg.MaxBytes {
		return ErrMempoolFull
	}

	// insert
	m.entries[key] = e
	m.total += e.Size
	heap.Push(&m.evictHeap, e)
	m.classCounts[e.PriorityClass]++
	ps.nonces[nonce] = now.Add(m.cfg.NonceTTL)
	return nil
}

// Select returns deterministic ordered transactions without removing them
func (m *Mempool) Select(maxCount, maxBytes int) []state.Transaction {
	m.mu.RLock()
	list := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		list = append(list, e)
	}
	m.mu.RUnlock()

	sort.Slice(list, func(i, j int) bool { return lessPriority(list[i], list[j]) })
	out := make([]state.Transaction, 0)
	var total int
	for _, e := range list {
		if maxCount > 0 && len(out) >= maxCount {
			break
		}
		if maxBytes > 0 && total+e.Size > maxBytes {
			break
		}
		out = append(out, e.Tx)
		total += e.Size
	}
	return out
}

// Remove removes transactions by content hash
func (m *Mempool) Remove(hashes ...[32]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range hashes {
		key := hex.EncodeToString(h[:])
		if e, ok := m.entries[key]; ok {
			delete(m.entries, key)
			m.total -= e.Size
			m.decClassCount(e.PriorityClass)
		}
	}
	// rebuild eviction heap to drop stale pointers (costly but safe and deterministic)
	m.evictHeap = make(minHeap, 0, len(m.entries))
	for _, e := range m.entries {
		heap.Push(&m.evictHeap, e)
	}
}

// RemoveByProducerNonces removes all mempool entries matching any producer_id+nonce pair.
func (m *Mempool) RemoveByProducerNonces(keys []ProducerNonce) {
	if len(keys) == 0 {
		return
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if len(k.ProducerID) == 0 || len(k.Nonce) == 0 {
			continue
		}
		id := hex.EncodeToString(k.ProducerID) + ":" + hex.EncodeToString(k.Nonce)
		set[id] = struct{}{}
	}
	if len(set) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	removed := false
	for key, e := range m.entries {
		id := hex.EncodeToString(e.ProducerID) + ":" + hex.EncodeToString(e.Nonce)
		if _, ok := set[id]; ok {
			delete(m.entries, key)
			m.total -= e.Size
			m.decClassCount(e.PriorityClass)
			removed = true
		}
	}
	if !removed {
		return
	}
	// Rebuild eviction heap to drop stale pointers.
	m.rebuildEvictHeapLocked()
}

// Stats returns current stats snapshot
func (m *Mempool) Stats() (count int, bytes int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries), m.total
}

// StatsDetailed returns count, total bytes, and the oldest transaction timestamp (unix seconds).
func (m *Mempool) StatsDetailed() (count int, bytes int, oldestTimestamp int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count = len(m.entries)
	bytes = m.total
	oldestTimestamp = 0
	for _, e := range m.entries {
		if oldestTimestamp == 0 || e.Ts < oldestTimestamp {
			oldestTimestamp = e.Ts
		}
	}
	return
}

// ClassStats returns current transaction counts per priority class.
func (m *Mempool) ClassStats() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]int, len(m.classCounts))
	for k, v := range m.classCounts {
		out[k] = v
	}
	return out
}

func (m *Mempool) classCap(class string) int {
	switch normalizePriorityClass(class) {
	case "p0":
		return m.cfg.MaxTxsP0
	case "p1":
		return m.cfg.MaxTxsP1
	default:
		return m.cfg.MaxTxsP2
	}
}

func (m *Mempool) decClassCount(class string) {
	normalized := normalizePriorityClass(class)
	if m.classCounts[normalized] > 0 {
		m.classCounts[normalized]--
	}
}

func normalizePriorityClass(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "p0":
		return "p0"
	case "p1":
		return "p1"
	default:
		return "p2"
	}
}

func classRank(class string) int {
	switch normalizePriorityClass(class) {
	case "p0":
		return 3
	case "p1":
		return 2
	default:
		return 1
	}
}

func (m *Mempool) weakestInClassLocked(class string) (string, *entry) {
	class = normalizePriorityClass(class)
	var (
		weakestKey string
		weakest    *entry
	)
	for key, candidate := range m.entries {
		if candidate == nil || normalizePriorityClass(candidate.PriorityClass) != class {
			continue
		}
		if weakest == nil || lessPriority(weakest, candidate) {
			weakest = candidate
			weakestKey = key
		}
	}
	return weakestKey, weakest
}

func (m *Mempool) rebuildEvictHeapLocked() {
	m.evictHeap = make(minHeap, 0, len(m.entries))
	for _, e := range m.entries {
		heap.Push(&m.evictHeap, e)
	}
}
