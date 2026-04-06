package main

import "backend/pkg/utils"

func sanitizeMempoolClassCaps(logger *utils.Logger, maxTxs int, p0 int, p1 int, p2 int) (int, int, int) {
	if p0 < 0 {
		if logger != nil {
			logger.Warn("Invalid MEMPOOL_MAX_TXS_P0; clamping to 0", utils.ZapInt("value", p0))
		}
		p0 = 0
	}
	if p1 < 0 {
		if logger != nil {
			logger.Warn("Invalid MEMPOOL_MAX_TXS_P1; clamping to 0", utils.ZapInt("value", p1))
		}
		p1 = 0
	}
	if p2 < 0 {
		if logger != nil {
			logger.Warn("Invalid MEMPOOL_MAX_TXS_P2; clamping to 0", utils.ZapInt("value", p2))
		}
		p2 = 0
	}
	if maxTxs > 0 {
		total := p0 + p1 + p2
		if total > maxTxs && logger != nil {
			logger.Warn("Per-class mempool caps exceed MEMPOOL_MAX_TXS; effective pressure may increase",
				utils.ZapInt("mempool_max_txs", maxTxs),
				utils.ZapInt("class_caps_total", total))
		}
	}
	return p0, p1, p2
}
