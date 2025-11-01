package api

import (
	"context"
	"encoding/hex"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	consapi "backend/pkg/consensus/api"
	"backend/pkg/consensus/types"
	"backend/pkg/p2p"
	"backend/pkg/utils"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	networkOverviewTimeout   = 5 * time.Second
	peerActivityEvaluation   = 30 * time.Second
	minNodeAliasLength       = 12
	suspiciousStaleDuration1 = 2 * time.Minute
	suspiciousStaleDuration2 = 5 * time.Minute
)

// handleNetworkOverview handles GET /network/overview
func (s *Server) handleNetworkOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.engine == nil {
		writeErrorFromUtils(w, r, NewUnavailableError("consensus engine"))
		return
	}

	timeout := s.config.RequestTimeout
	if timeout <= 0 || timeout > networkOverviewTimeout {
		timeout = networkOverviewTimeout
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	overview, err := s.buildNetworkOverview(ctx, time.Now().UTC())
	if err != nil {
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "failed to build network overview",
				utils.ZapError(err))
		}
		writeErrorResponse(w, r, "NETWORK_OVERVIEW_ERROR", "failed to aggregate network overview", http.StatusBadGateway)
		return
	}

	writeJSONResponse(w, r, NewSuccessResponse(overview), http.StatusOK)
}

func (s *Server) buildNetworkOverview(ctx context.Context, now time.Time) (*NetworkOverviewResponse, error) {
	status := s.engine.GetStatus()
	validators := s.engine.ListValidators()

	var routerStats p2p.RouterStats
	if s.p2pRouter != nil {
		routerStats = s.p2pRouter.GetNetworkStats()
	}

	totalPeers := len(validators)
	if routerStats.PeerCount > totalPeers {
		totalPeers = routerStats.PeerCount
	}

	nodes, peerResolver := s.buildNetworkNodes(validators, routerStats, now)
	edges := toNetworkEdges(routerStats.Edges, peerResolver)
	selfID := ""
	if routerStats.Self != "" {
		peerKey := strings.ToLower(routerStats.Self.String())
		if mapped, ok := peerResolver[peerKey]; ok && mapped != "" {
			selfID = mapped
		} else {
			selfID = routerStats.Self.String()
		}
	}

	leaderID := encodeValidatorID(status.CurrentLeader)

	overview := &NetworkOverviewResponse{
		ConnectedPeers:   routerStats.PeerCount,
		TotalPeers:       totalPeers,
		ExpectedPeers:    len(validators),
		AverageLatencyMs: routerStats.AvgLatencyMs,
		ConsensusRound:   status.Height,
		LeaderStability:  computeLeaderStability(status.Metrics.ViewChanges),
		Phase:            deriveConsensusPhase(status),
		Leader:           s.resolveLeaderAlias(status.CurrentLeader, validators),
		LeaderID:         leaderID,
		Nodes:            nodes,
		VotingStatus:     buildVotingStatus(validators),
		Edges:            s.mergeInferredEdges(edges, peerResolver, routerStats.Self, validators, selfID, now),
		Self:             selfID,
		InboundRateBps:   routerStats.InboundRateBps,
		OutboundRateBps:  routerStats.OutboundRateBps,
		UpdatedAt:        now,
	}

	return overview, nil
}

func (s *Server) buildNetworkNodes(validators []types.ValidatorInfo, stats p2p.RouterStats, now time.Time) ([]NetworkNodeDTO, map[string]string) {
	peerMetrics := make(map[string]p2p.RouterPeerStats, len(stats.Peers))
	for _, peerStat := range stats.Peers {
		peerMetrics[strings.ToLower(peerStat.ID.String())] = peerStat
	}

	nodes := make([]NetworkNodeDTO, 0, len(validators))
	peerResolver := make(map[string]string, len(validators))
	for idx, v := range validators {
		uptime := reputationToUptime(v.Reputation)
		status := deriveNodeStatus(uptime, v.IsActive)
		alias := s.resolveValidatorAlias(v, idx)

		peerID := strings.ToLower(derivePeerIDFromPublicKey(v.PublicKey))
		if peerID != "" {
			peerResolver[peerID] = encodeValidatorID(v.ID)
		}
		peerStat, ok := peerMetrics[peerID]
		latencyMs := 0.0
		throughput := uint64(0)
		lastSeen := v.LastSeen
		inRate := 0.0
		if ok {
			if peerStat.Status != "" {
				status = peerStat.Status
			}
			if peerStat.LatencyMs > 0 {
				latencyMs = peerStat.LatencyMs
			}
			throughput = peerStat.BytesIn
			if peerStat.RateBps > 0 {
				inRate = peerStat.RateBps
			}
			if !peerStat.LastSeen.IsZero() {
				lastSeen = peerStat.LastSeen
			}
		}

		var lastSeenPtr *time.Time
		if !lastSeen.IsZero() {
			ts := lastSeen.UTC()
			lastSeenPtr = &ts
		}

		nodes = append(nodes, NetworkNodeDTO{
			ID:              encodeValidatorID(v.ID),
			Name:            alias,
			Status:          status,
			Latency:         latencyMs,
			Uptime:          uptime,
			ThroughputBytes: throughput,
			LastSeen:        lastSeenPtr,
			InboundRateBps:  inRate,
		})
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})

	return nodes, peerResolver
}

func toNetworkEdges(edges []p2p.RouterEdge, resolver map[string]string) []NetworkEdgeDTO {
	if len(edges) == 0 {
		return nil
	}
	out := make([]NetworkEdgeDTO, 0, len(edges))
	for _, edge := range edges {
		source := edge.Source.String()
		if mapped, ok := resolver[strings.ToLower(source)]; ok && mapped != "" {
			source = mapped
		}
		target := edge.Target.String()
		if mapped, ok := resolver[strings.ToLower(target)]; ok && mapped != "" {
			target = mapped
		}
		direction := edge.Direction
		if direction == "" {
			direction = "unknown"
		}
		status := edge.Status
		if status == "" {
			status = "live"
		}
		confidence := edge.Confidence
		if confidence == "" {
			confidence = "observed"
		}
		reportedBy := ""
		if edge.ReportedBy != "" {
			reportedBy = edge.ReportedBy.String()
		}
		var updatedAt *time.Time
		if !edge.UpdatedAt.IsZero() {
			ts := edge.UpdatedAt.UTC()
			updatedAt = &ts
		}
		out = append(out, NetworkEdgeDTO{
			Source:     source,
			Target:     target,
			Direction:  direction,
			Status:     status,
			Confidence: confidence,
			ReportedBy: reportedBy,
			UpdatedAt:  updatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source == out[j].Source {
			return out[i].Target < out[j].Target
		}
		return out[i].Source < out[j].Source
	})
	return out
}

func (s *Server) mergeInferredEdges(edges []NetworkEdgeDTO, resolver map[string]string, selfPeer peer.ID, validators []types.ValidatorInfo, selfID string, now time.Time) []NetworkEdgeDTO {
	existing := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		key := edge.Source + "->" + edge.Target
		existing[key] = struct{}{}
	}
	selfLabel := selfID
	if selfLabel == "" && selfPeer != "" {
		if mapped, ok := resolver[strings.ToLower(selfPeer.String())]; ok && mapped != "" {
			selfLabel = mapped
		} else {
			selfLabel = selfPeer.String()
		}
	}
	if selfLabel == "" {
		return edges
	}

	appendix := make([]NetworkEdgeDTO, 0)
	for _, v := range validators {
		validatorID := encodeValidatorID(v.ID)
		if validatorID == "" || validatorID == selfLabel {
			continue
		}
		key := selfLabel + "->" + validatorID
		if _, ok := existing[key]; ok {
			continue
		}
		// add inferred offline edge from current node perspective
		ts := now.UTC()
		appendix = append(appendix, NetworkEdgeDTO{
			Source:     selfLabel,
			Target:     validatorID,
			Direction:  "unknown",
			Status:     "offline",
			Confidence: "inferred",
			ReportedBy: selfLabel,
			UpdatedAt:  &ts,
		})
	}
	if len(appendix) == 0 {
		return edges
	}
	combined := append(edges, appendix...)
	sort.Slice(combined, func(i, j int) bool {
		if combined[i].Source == combined[j].Source {
			return combined[i].Target < combined[j].Target
		}
		return combined[i].Source < combined[j].Source
	})
	return combined
}

func buildVotingStatus(validators []types.ValidatorInfo) []VotingStatusDTO {
	statuses := make([]VotingStatusDTO, 0, len(validators))
	for _, v := range validators {
		statuses = append(statuses, VotingStatusDTO{
			NodeID: encodeValidatorID(v.ID),
			Voting: v.IsActive,
		})
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].NodeID < statuses[j].NodeID
	})
	return statuses
}

func derivePeerIDFromPublicKey(pub []byte) string {
	if len(pub) == 0 {
		return ""
	}
	key, err := crypto.UnmarshalEd25519PublicKey(pub)
	if err != nil {
		return ""
	}
	pid, err := peer.IDFromPublicKey(key)
	if err != nil {
		return ""
	}
	return pid.String()
}

func (s *Server) resolveValidatorAlias(info types.ValidatorInfo, index int) string {
	shortHex := encodeHex(info.PublicKey)
	if len(shortHex) > minNodeAliasLength {
		shortHex = shortHex[:minNodeAliasLength]
	}
	if s == nil {
		return shortHex
	}
	keyHex := strings.ToLower(encodeValidatorID(info.ID))
	if alias := s.nodeAliases[keyHex]; alias != "" {
		return alias
	}
	pubHex := strings.ToLower(hex.EncodeToString(info.PublicKey))
	if alias := s.nodeAliases[pubHex]; alias != "" {
		return alias
	}
	peerID := strings.ToLower(derivePeerIDFromPublicKey(info.PublicKey))
	if alias := s.nodeAliases[peerID]; alias != "" {
		return alias
	}
	if index < len(s.nodeAliasList) {
		candidate := s.nodeAliasList[index]
		if candidate != "" {
			return candidate
		}
	}
	return shortHex
}

func (s *Server) resolveLeaderAlias(id types.ValidatorID, validators []types.ValidatorInfo) string {
	if s == nil {
		return encodeValidatorID(id)
	}
	keyHex := strings.ToLower(encodeValidatorID(id))
	if alias := s.nodeAliases[keyHex]; alias != "" {
		return alias
	}
	for idx, v := range validators {
		if v.ID == id {
			return s.resolveValidatorAlias(v, idx)
		}
	}
	return encodeValidatorID(id)
}

func deriveConsensusPhase(status consapi.EngineStatus) string {
	switch {
	case !status.Running:
		return "stopped"
	case status.Running && status.Metrics.BlocksCommitted == 0:
		return "initializing"
	default:
		return "active"
	}
}

func computeLeaderStability(viewChanges uint64) float64 {
	if viewChanges == 0 {
		return 100
	}
	stability := 100 - math.Min(float64(viewChanges), 100)
	if stability < 0 {
		return 0
	}
	return stability
}

func encodeValidatorID(id types.ValidatorID) string {
	if isZeroValidatorID(id) {
		return ""
	}
	return encodeHex(id[:])
}

func isZeroValidatorID(id types.ValidatorID) bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

func reputationToUptime(rep float64) float64 {
	switch {
	case rep <= 0:
		return 0
	case rep <= 1:
		return clampFloat(rep*100, 0, 100)
	case rep >= 100:
		return 100
	default:
		return clampFloat(rep, 0, 100)
	}
}

func deriveNodeStatus(uptime float64, active bool) string {
	if !active {
		return "critical"
	}
	switch {
	case uptime >= 95:
		return "healthy"
	case uptime >= 80:
		return "warning"
	default:
		return "critical"
	}
}

func clampFloat(value, minVal, maxVal float64) float64 {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}
