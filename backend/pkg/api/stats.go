package api

import (
	"context"
	"net/http"

	"backend/pkg/consensus/types"
)

// handleStats handles GET /stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	// Build statistics response
	response := StatsResponse{
		Chain:     s.getChainStats(ctx),
		Consensus: s.getConsensusStats(ctx),
		Mempool:   s.getMempoolStats(ctx),
		Network:   s.getNetworkStats(ctx),
	}

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

// getChainStats returns blockchain statistics
func (s *Server) getChainStats(ctx context.Context) *ChainStats {
	stats := &ChainStats{}

	if s.stateStore != nil {
		latest := s.stateStore.Latest()
		stats.Height = latest
		stats.StateVersion = latest
	}

	return stats
}

// getConsensusStats returns consensus statistics
func (s *Server) getConsensusStats(ctx context.Context) *ConsensusStats {
	stats := &ConsensusStats{}

	if s.engine == nil {
		return stats
	}

	st := s.engine.GetStatus()
	stats.View = st.View
	stats.Round = st.Height

	validators := s.engine.ListValidators()
	stats.ValidatorCount = len(validators)
	if stats.ValidatorCount > 0 {
		stats.QuorumSize = (stats.ValidatorCount*2)/3 + 1
	}

	var zeroLeader types.ValidatorID
	if st.CurrentLeader != zeroLeader {
		stats.CurrentLeader = encodeHex(st.CurrentLeader[:])
	}

	return stats
}

// getMempoolStats returns mempool statistics
func (s *Server) getMempoolStats(ctx context.Context) *MempoolStats {
	stats := &MempoolStats{}

	if s.mempool != nil {
        count, bytes := s.mempool.Stats()
        stats.PendingTransactions = count
        stats.SizeBytes = int64(bytes)
	}

	return stats
}

// getNetworkStats returns network statistics (optional)
func (s *Server) getNetworkStats(ctx context.Context) *NetworkStats {
	// Network stats are optional and may not be available
	// In production, this would come from P2P layer
	
	stats := &NetworkStats{
		PeerCount:     0,
		InboundPeers:  0,
		OutboundPeers: 0,
		BytesReceived: 0,
		BytesSent:     0,
	}

	return stats
}
