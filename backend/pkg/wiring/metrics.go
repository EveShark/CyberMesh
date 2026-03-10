package wiring

import (
	"sync/atomic"
	"time"

	"backend/pkg/state"
	"backend/pkg/utils"
)

// Metrics tracks wiring layer performance and events.
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
	ValidationFailures   uint64
	ParentHashMismatches uint64

	// Timing metrics (in nanoseconds)
	LastCommitDuration   int64
	TotalCommitDuration  int64
	LastProposalDuration int64

	// ApplyBlock metrics
	ApplyBlockRuns                   uint64
	ApplyBlockEventTxs               uint64
	ApplyBlockEvidenceTxs            uint64
	ApplyBlockPolicyTxs              uint64
	ApplyBlockTotalBuckets           []utils.HistogramBucket
	ApplyBlockTotalCount             uint64
	ApplyBlockTotalSumMs             float64
	ApplyBlockTotalP95Ms             float64
	ApplyBlockValidateBuckets        []utils.HistogramBucket
	ApplyBlockValidateCount          uint64
	ApplyBlockValidateSumMs          float64
	ApplyBlockValidateP95Ms          float64
	ApplyBlockNonceCheckBuckets      []utils.HistogramBucket
	ApplyBlockNonceCheckCount        uint64
	ApplyBlockNonceCheckSumMs        float64
	ApplyBlockNonceCheckP95Ms        float64
	ApplyBlockReducerEventBuckets    []utils.HistogramBucket
	ApplyBlockReducerEventCount      uint64
	ApplyBlockReducerEventSumMs      float64
	ApplyBlockReducerEventP95Ms      float64
	ApplyBlockReducerEvidenceBuckets []utils.HistogramBucket
	ApplyBlockReducerEvidenceCount   uint64
	ApplyBlockReducerEvidenceSumMs   float64
	ApplyBlockReducerEvidenceP95Ms   float64
	ApplyBlockReducerPolicyBuckets   []utils.HistogramBucket
	ApplyBlockReducerPolicyCount     uint64
	ApplyBlockReducerPolicySumMs     float64
	ApplyBlockReducerPolicyP95Ms     float64
	ApplyBlockCommitStateBuckets     []utils.HistogramBucket
	ApplyBlockCommitStateCount       uint64
	ApplyBlockCommitStateSumMs       float64
	ApplyBlockCommitStateP95Ms       float64

	// State metrics
	LastCommittedHeight  uint64
	LastCommittedVersion uint64

	// Mempool metrics
	MempoolSize      int64
	MempoolSizeBytes int64

	applyBlockTotalHist           *utils.LatencyHistogram
	applyBlockValidateHist        *utils.LatencyHistogram
	applyBlockNonceCheckHist      *utils.LatencyHistogram
	applyBlockReducerEventHist    *utils.LatencyHistogram
	applyBlockReducerEvidenceHist *utils.LatencyHistogram
	applyBlockReducerPolicyHist   *utils.LatencyHistogram
	applyBlockCommitStateHist     *utils.LatencyHistogram
}

func NewMetrics() *Metrics {
	buckets := []float64{1, 5, 20, 50, 100, 250, 500, 1000, 2500, 5000}
	return &Metrics{
		applyBlockTotalHist:           utils.NewLatencyHistogram(buckets),
		applyBlockValidateHist:        utils.NewLatencyHistogram(buckets),
		applyBlockNonceCheckHist:      utils.NewLatencyHistogram(buckets),
		applyBlockReducerEventHist:    utils.NewLatencyHistogram(buckets),
		applyBlockReducerEvidenceHist: utils.NewLatencyHistogram(buckets),
		applyBlockReducerPolicyHist:   utils.NewLatencyHistogram(buckets),
		applyBlockCommitStateHist:     utils.NewLatencyHistogram(buckets),
	}
}

func (m *Metrics) IncrementProposalsAttempted() {
	atomic.AddUint64(&m.ProposalsAttempted, 1)
}

func (m *Metrics) IncrementProposalsSucceeded() {
	atomic.AddUint64(&m.ProposalsSucceeded, 1)
}

func (m *Metrics) IncrementProposalsFailed() {
	atomic.AddUint64(&m.ProposalsFailed, 1)
}

func (m *Metrics) IncrementBlocksCommitted() {
	atomic.AddUint64(&m.BlocksCommitted, 1)
}

func (m *Metrics) AddTransactionsCommitted(count uint64) {
	atomic.AddUint64(&m.TransactionsCommitted, count)
}

func (m *Metrics) IncrementCommitErrors() {
	atomic.AddUint64(&m.CommitErrors, 1)
}

func (m *Metrics) IncrementValidationFailures() {
	atomic.AddUint64(&m.ValidationFailures, 1)
}

func (m *Metrics) IncrementParentHashMismatches() {
	atomic.AddUint64(&m.ParentHashMismatches, 1)
}

func (m *Metrics) RecordCommitDuration(d time.Duration) {
	nanos := d.Nanoseconds()
	atomic.StoreInt64(&m.LastCommitDuration, nanos)
	atomic.AddInt64(&m.TotalCommitDuration, nanos)
}

func (m *Metrics) RecordProposalDuration(d time.Duration) {
	atomic.StoreInt64(&m.LastProposalDuration, d.Nanoseconds())
}

func (m *Metrics) RecordApplyBlockMetrics(am state.ApplyBlockMetrics) {
	if m == nil {
		return
	}
	atomic.AddUint64(&m.ApplyBlockRuns, 1)
	atomic.AddUint64(&m.ApplyBlockEventTxs, uint64(am.EventTxs))
	atomic.AddUint64(&m.ApplyBlockEvidenceTxs, uint64(am.EvidenceTxs))
	atomic.AddUint64(&m.ApplyBlockPolicyTxs, uint64(am.PolicyTxs))
	record := func(hist *utils.LatencyHistogram, d time.Duration) {
		if hist != nil {
			hist.Observe(float64(d) / float64(time.Millisecond))
		}
	}
	record(m.applyBlockTotalHist, am.Total)
	record(m.applyBlockValidateHist, am.Validate)
	record(m.applyBlockNonceCheckHist, am.NonceCheck)
	record(m.applyBlockReducerEventHist, am.ReducerEvent)
	record(m.applyBlockReducerEvidenceHist, am.ReducerEvidence)
	record(m.applyBlockReducerPolicyHist, am.ReducerPolicy)
	record(m.applyBlockCommitStateHist, am.CommitState)
}

func (m *Metrics) UpdateLastCommittedHeight(height uint64) {
	atomic.StoreUint64(&m.LastCommittedHeight, height)
}

func (m *Metrics) UpdateLastCommittedVersion(version uint64) {
	atomic.StoreUint64(&m.LastCommittedVersion, version)
}

func (m *Metrics) UpdateMempoolStats(size int, sizeBytes int) {
	atomic.StoreInt64(&m.MempoolSize, int64(size))
	atomic.StoreInt64(&m.MempoolSizeBytes, int64(sizeBytes))
}

func (m *Metrics) GetSnapshot() Metrics {
	if m == nil {
		return Metrics{}
	}
	snap := Metrics{
		ProposalsAttempted:    atomic.LoadUint64(&m.ProposalsAttempted),
		ProposalsSucceeded:    atomic.LoadUint64(&m.ProposalsSucceeded),
		ProposalsFailed:       atomic.LoadUint64(&m.ProposalsFailed),
		BlocksCommitted:       atomic.LoadUint64(&m.BlocksCommitted),
		TransactionsCommitted: atomic.LoadUint64(&m.TransactionsCommitted),
		CommitErrors:          atomic.LoadUint64(&m.CommitErrors),
		ValidationFailures:    atomic.LoadUint64(&m.ValidationFailures),
		ParentHashMismatches:  atomic.LoadUint64(&m.ParentHashMismatches),
		LastCommitDuration:    atomic.LoadInt64(&m.LastCommitDuration),
		TotalCommitDuration:   atomic.LoadInt64(&m.TotalCommitDuration),
		LastProposalDuration:  atomic.LoadInt64(&m.LastProposalDuration),
		ApplyBlockRuns:        atomic.LoadUint64(&m.ApplyBlockRuns),
		ApplyBlockEventTxs:    atomic.LoadUint64(&m.ApplyBlockEventTxs),
		ApplyBlockEvidenceTxs: atomic.LoadUint64(&m.ApplyBlockEvidenceTxs),
		ApplyBlockPolicyTxs:   atomic.LoadUint64(&m.ApplyBlockPolicyTxs),
		LastCommittedHeight:   atomic.LoadUint64(&m.LastCommittedHeight),
		LastCommittedVersion:  atomic.LoadUint64(&m.LastCommittedVersion),
		MempoolSize:           atomic.LoadInt64(&m.MempoolSize),
		MempoolSizeBytes:      atomic.LoadInt64(&m.MempoolSizeBytes),
	}
	fill := func(hist *utils.LatencyHistogram, buckets *[]utils.HistogramBucket, count *uint64, sum *float64, p95 *float64) {
		if hist == nil {
			return
		}
		*buckets, *count, *sum = hist.Snapshot()
		*p95 = hist.Quantile(0.95)
	}
	fill(m.applyBlockTotalHist, &snap.ApplyBlockTotalBuckets, &snap.ApplyBlockTotalCount, &snap.ApplyBlockTotalSumMs, &snap.ApplyBlockTotalP95Ms)
	fill(m.applyBlockValidateHist, &snap.ApplyBlockValidateBuckets, &snap.ApplyBlockValidateCount, &snap.ApplyBlockValidateSumMs, &snap.ApplyBlockValidateP95Ms)
	fill(m.applyBlockNonceCheckHist, &snap.ApplyBlockNonceCheckBuckets, &snap.ApplyBlockNonceCheckCount, &snap.ApplyBlockNonceCheckSumMs, &snap.ApplyBlockNonceCheckP95Ms)
	fill(m.applyBlockReducerEventHist, &snap.ApplyBlockReducerEventBuckets, &snap.ApplyBlockReducerEventCount, &snap.ApplyBlockReducerEventSumMs, &snap.ApplyBlockReducerEventP95Ms)
	fill(m.applyBlockReducerEvidenceHist, &snap.ApplyBlockReducerEvidenceBuckets, &snap.ApplyBlockReducerEvidenceCount, &snap.ApplyBlockReducerEvidenceSumMs, &snap.ApplyBlockReducerEvidenceP95Ms)
	fill(m.applyBlockReducerPolicyHist, &snap.ApplyBlockReducerPolicyBuckets, &snap.ApplyBlockReducerPolicyCount, &snap.ApplyBlockReducerPolicySumMs, &snap.ApplyBlockReducerPolicyP95Ms)
	fill(m.applyBlockCommitStateHist, &snap.ApplyBlockCommitStateBuckets, &snap.ApplyBlockCommitStateCount, &snap.ApplyBlockCommitStateSumMs, &snap.ApplyBlockCommitStateP95Ms)
	return snap
}
