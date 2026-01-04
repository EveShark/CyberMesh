package block

import "time"

type Config struct {
	MaxTxsPerBlock                int
	MaxBlockBytes                 int
	MinMempoolTxs                 int
	BuildInterval                 time.Duration
	MempoolCapacity               int
	BackpressureThreshold         float64
	LatencyThresholdSeconds       int64
	MaxTxsBackpressure            int
	MaxTxsLatency                 int
}
