package block

import "time"

type Config struct {
	MaxTxsPerBlock           int
	MaxBlockBytes            int
	ProposalSizeReserveBytes int
	MinMempoolTxs            int
	BuildInterval            time.Duration
	PolicyPriorityMinTxs     int
	P0PriorityMinTxs         int
	MempoolCapacity          int
	BackpressureThreshold    float64
	LatencyThresholdSeconds  int64
	MaxTxsBackpressure       int
	MaxTxsLatency            int
	FairShareEnabled         bool
	FairShareMaxPerProducer  int
}
