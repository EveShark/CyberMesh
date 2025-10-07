package wiring

import (
	"sync/atomic"
	"time"
)

// Metrics tracks wiring layer performance and events
type Metrics struct {
	// Proposal metrics
	ProposalsAttempted uint64
	ProposalsSucceeded uint64
	ProposalsFailed    uint64

	// Commit metrics
	BlocksCommitted       uint64
	TransactionsCommitted uint64
	CommitErrors          uint64

	// Validation metrics
	ValidationFailures uint64

	// Timing metrics (in nanoseconds)
	LastCommitDuration   int64
	TotalCommitDuration  int64
	LastProposalDuration int64

	// State metrics
	LastCommittedHeight  uint64
	LastCommittedVersion uint64

	// Mempool metrics
	MempoolSize      int64
	MempoolSizeBytes int64
}

// IncrementProposalsAttempted atomically increments the counter
func (m *Metrics) IncrementProposalsAttempted() {
	atomic.AddUint64(&m.ProposalsAttempted, 1)
}

// IncrementProposalsSucceeded atomically increments the counter
func (m *Metrics) IncrementProposalsSucceeded() {
	atomic.AddUint64(&m.ProposalsSucceeded, 1)
}

// IncrementProposalsFailed atomically increments the counter
func (m *Metrics) IncrementProposalsFailed() {
	atomic.AddUint64(&m.ProposalsFailed, 1)
}

// IncrementBlocksCommitted atomically increments the counter
func (m *Metrics) IncrementBlocksCommitted() {
	atomic.AddUint64(&m.BlocksCommitted, 1)
}

// AddTransactionsCommitted atomically adds to the counter
func (m *Metrics) AddTransactionsCommitted(count uint64) {
	atomic.AddUint64(&m.TransactionsCommitted, count)
}

// IncrementCommitErrors atomically increments the counter
func (m *Metrics) IncrementCommitErrors() {
	atomic.AddUint64(&m.CommitErrors, 1)
}

// IncrementValidationFailures atomically increments the counter
func (m *Metrics) IncrementValidationFailures() {
	atomic.AddUint64(&m.ValidationFailures, 1)
}

// RecordCommitDuration records the duration of a commit operation
func (m *Metrics) RecordCommitDuration(d time.Duration) {
	nanos := d.Nanoseconds()
	atomic.StoreInt64(&m.LastCommitDuration, nanos)
	atomic.AddInt64(&m.TotalCommitDuration, nanos)
}

// RecordProposalDuration records the duration of a proposal operation
func (m *Metrics) RecordProposalDuration(d time.Duration) {
	atomic.StoreInt64(&m.LastProposalDuration, d.Nanoseconds())
}

// UpdateLastCommittedHeight atomically updates the last committed height
func (m *Metrics) UpdateLastCommittedHeight(height uint64) {
	atomic.StoreUint64(&m.LastCommittedHeight, height)
}

// UpdateLastCommittedVersion atomically updates the last committed version
func (m *Metrics) UpdateLastCommittedVersion(version uint64) {
	atomic.StoreUint64(&m.LastCommittedVersion, version)
}

// UpdateMempoolStats updates mempool-related metrics (not atomic, caller should handle concurrency)
func (m *Metrics) UpdateMempoolStats(size int, sizeBytes int) {
	atomic.StoreInt64(&m.MempoolSize, int64(size))
	atomic.StoreInt64(&m.MempoolSizeBytes, int64(sizeBytes))
}

// GetSnapshot returns a snapshot of current metrics
func (m *Metrics) GetSnapshot() Metrics {
	return Metrics{
		ProposalsAttempted:    atomic.LoadUint64(&m.ProposalsAttempted),
		ProposalsSucceeded:    atomic.LoadUint64(&m.ProposalsSucceeded),
		ProposalsFailed:       atomic.LoadUint64(&m.ProposalsFailed),
		BlocksCommitted:       atomic.LoadUint64(&m.BlocksCommitted),
		TransactionsCommitted: atomic.LoadUint64(&m.TransactionsCommitted),
		CommitErrors:          atomic.LoadUint64(&m.CommitErrors),
		ValidationFailures:    atomic.LoadUint64(&m.ValidationFailures),
		LastCommitDuration:    atomic.LoadInt64(&m.LastCommitDuration),
		TotalCommitDuration:   atomic.LoadInt64(&m.TotalCommitDuration),
		LastProposalDuration:  atomic.LoadInt64(&m.LastProposalDuration),
		LastCommittedHeight:   atomic.LoadUint64(&m.LastCommittedHeight),
		LastCommittedVersion:  atomic.LoadUint64(&m.LastCommittedVersion),
		MempoolSize:           atomic.LoadInt64(&m.MempoolSize),
		MempoolSizeBytes:      atomic.LoadInt64(&m.MempoolSizeBytes),
	}
}
