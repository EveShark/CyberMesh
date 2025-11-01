package mempool

import (
	"container/heap"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"backend/pkg/state"
	"backend/pkg/utils"
)

var (
	ErrDuplicate   = errors.New("duplicate transaction")
	ErrRateLimited = errors.New("producer rate limited")
	ErrMempoolFull = errors.New("mempool at capacity")
	ErrInvalidTx   = errors.New("invalid transaction")
)

type Config struct {
	MaxTxs        int
	MaxBytes      int
	NonceTTL      time.Duration
	Skew          time.Duration
	RatePerSecond int
}

type entry struct {
	Tx         state.Transaction
	Hash       [32]byte
	ProducerID []byte
	Nonce      []byte
	Severity   uint8
	Confidence float64
	Ts         int64
	Size       int
}

// priority ordering: higher severity, higher confidence, earlier ts, lexicographic hash
func lessPriority(a, b *entry) bool {
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
	mu        sync.RWMutex
	log       *utils.Logger
	cfg       Config
	entries   map[string]*entry         // hexdigest(hash) -> entry
	total     int                       // bytes
	evictHeap minHeap                   // eviction heap (lowest priority at root)
	prod      map[string]*producerState // hex(producerID) -> state
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
	}
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
	Severity   uint8
	Confidence float64
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
	// Rate limiting
	if m.cfg.RatePerSecond > 0 && !ps.bucket.allow(now) {
		return ErrRateLimited
	}

	e := &entry{
		Tx:         tx,
		Hash:       h,
		ProducerID: env.ProducerID,
		Nonce:      env.Nonce,
		Severity:   meta.Severity,
		Confidence: meta.Confidence,
		Ts:         tx.Timestamp(),
		Size:       len(tx.Payload()),
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
		}
	}
	// rebuild eviction heap to drop stale pointers (costly but safe and deterministic)
	m.evictHeap = make(minHeap, 0, len(m.entries))
	for _, e := range m.entries {
		heap.Push(&m.evictHeap, e)
	}
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
