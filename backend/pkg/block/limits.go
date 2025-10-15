package block

import "time"

type Config struct {
	MaxTxsPerBlock int
	MaxBlockBytes  int
	MinMempoolTxs  int
	BuildInterval  time.Duration
}
