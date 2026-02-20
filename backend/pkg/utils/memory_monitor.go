package utils

import (
	"runtime"
	"sync"
)

type MemoryMonitor struct {
	mu              sync.Mutex
	lastLoggedMB    uint64
	thresholdsMB    []uint64
	logger          *Logger
}

func NewMemoryMonitor(logger *Logger) *MemoryMonitor {
	return &MemoryMonitor{
		thresholdsMB: []uint64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000},
		logger:       logger,
	}
}

func (m *MemoryMonitor) Check(mempoolTxs int, producerCount int, voteCount int, qcCount int, stateVersions int, p2pStreams int) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	
	allocMB := mem.Alloc / 1024 / 1024
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for _, threshold := range m.thresholdsMB {
		if allocMB >= threshold && m.lastLoggedMB < threshold {
			m.logger.Info("memory_threshold_crossed",
				ZapUint64("memory_mb", allocMB),
				ZapUint64("threshold_mb", threshold),
				ZapInt("mempool_txs", mempoolTxs),
				ZapInt("producers", producerCount),
				ZapInt("votes", voteCount),
				ZapInt("qcs", qcCount),
				ZapInt("state_versions", stateVersions),
				ZapInt("p2p_streams", p2pStreams),
				ZapInt("goroutines", runtime.NumGoroutine()),
				ZapUint32("gc_count", mem.NumGC),
				ZapUint64("sys_mb", mem.Sys/1024/1024))
			m.lastLoggedMB = threshold
			break
		}
	}
}

func (m *MemoryMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastLoggedMB = 0
}
