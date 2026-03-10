package state

import (
	"sync"
	"sync/atomic"
	"time"
)

type ApplyBlockMetrics struct {
	Total           time.Duration
	Validate        time.Duration
	NonceCheck      time.Duration
	ReducerEvent    time.Duration
	ReducerEvidence time.Duration
	ReducerPolicy   time.Duration
	CommitState     time.Duration
	EventTxs        int
	EvidenceTxs     int
	PolicyTxs       int
}

var (
	applyBlockObserverMu sync.RWMutex
	applyBlockObserver   func(ApplyBlockMetrics)
	applyBlockObserverID uint64
)

func SetApplyBlockObserver(fn func(ApplyBlockMetrics)) func() {
	applyBlockObserverMu.Lock()
	id := atomic.AddUint64(&applyBlockObserverID, 1)
	applyBlockObserver = fn
	applyBlockObserverMu.Unlock()
	return func() {
		applyBlockObserverMu.Lock()
		defer applyBlockObserverMu.Unlock()
		if applyBlockObserverID == id {
			applyBlockObserver = nil
		}
	}
}

func reportApplyBlockMetrics(m ApplyBlockMetrics) {
	applyBlockObserverMu.RLock()
	fn := applyBlockObserver
	applyBlockObserverMu.RUnlock()
	if fn != nil {
		fn(m)
	}
}
