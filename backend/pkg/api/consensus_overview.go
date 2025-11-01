package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	consapi "backend/pkg/consensus/api"
	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
	"backend/pkg/utils"
)

const (
	consensusOverviewTimeout = 5 * time.Second
	consensusProposalLimit   = 16
	consensusSuspicionCutoff = 40.0
	consensusTimelineBucket  = 30 * time.Second
)

// handleConsensusOverview handles GET /consensus/overview
func (s *Server) handleConsensusOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.engine == nil {
		writeErrorFromUtils(w, r, NewUnavailableError("consensus engine"))
		return
	}

	timeout := s.config.RequestTimeout
	if timeout <= 0 || timeout > consensusOverviewTimeout {
		timeout = consensusOverviewTimeout
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	overview, err := s.buildConsensusOverview(ctx, time.Now().UTC())
	if err != nil {
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "failed to build consensus overview",
				utils.ZapError(err))
		}
		writeErrorResponse(w, r, "CONSENSUS_OVERVIEW_ERROR", "failed to aggregate consensus overview", http.StatusBadGateway)
		return
	}

	writeJSONResponse(w, r, NewSuccessResponse(overview), http.StatusOK)
}

func (s *Server) buildConsensusOverview(ctx context.Context, now time.Time) (*ConsensusOverviewResponse, error) {
	status := s.engine.GetStatus()
	validators := s.engine.ListValidators()

	activePeers := 0
	if s.p2pRouter != nil {
		activePeers = s.p2pRouter.GetActivePeerCount(peerActivityEvaluation)
	}

	var latestHeight uint64
	if s.storage != nil {
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if h, err := s.storage.GetLatestHeight(queryCtx); err == nil {
			latestHeight = h
		}
		cancel()
	}

	proposals := s.loadRecentProposals(ctx, now, latestHeight, consensusProposalLimit)
	votes := s.buildConsensusVoteTimeline(ctx, now, latestHeight, consensusProposalLimit, status, proposals)
	suspicious := s.buildSuspiciousNodes(ctx, validators, now)

	overview := &ConsensusOverviewResponse{
		Leader:          s.resolveLeaderAlias(status.CurrentLeader, validators),
		LeaderID:        encodeValidatorID(status.CurrentLeader),
		Term:            status.View,
		Phase:           deriveConsensusPhase(status),
		ActivePeers:     activePeers,
		QuorumSize:      computeQuorumSize(len(validators)),
		Proposals:       proposals,
		Votes:           votes,
		SuspiciousNodes: suspicious,
		UpdatedAt:       now,
	}

	if overview.Proposals == nil {
		overview.Proposals = []ConsensusProposalDTO{}
	}
	if overview.Votes == nil {
		overview.Votes = []ConsensusVoteDTO{}
	}
	if overview.SuspiciousNodes == nil {
		overview.SuspiciousNodes = []SuspiciousNodeDTO{}
	}

	return overview, nil
}

func (s *Server) loadRecentProposals(ctx context.Context, now time.Time, latestHeight uint64, limit int) []ConsensusProposalDTO {
	if limit <= 0 || s.storage == nil {
		return nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var minHeight uint64
	if latestHeight >= uint64(limit) && latestHeight > 0 {
		minHeight = latestHeight - uint64(limit) + 1
	}

	records, err := s.storage.ListProposals(queryCtx, minHeight, limit)
	if err != nil {
		return nil
	}

	proposals := make([]ConsensusProposalDTO, 0, len(records))
	for _, rec := range records {
		var proposal messages.Proposal
		if err := utils.CBORUnmarshal(rec.Data, &proposal); err != nil {
			continue
		}

		proposals = append(proposals, ConsensusProposalDTO{
			Block:     rec.Height,
			View:      rec.View,
			Hash:      encodeHex(rec.Hash),
			Proposer:  encodeHex(rec.Proposer),
			Timestamp: proposal.Timestamp.UnixMilli(),
		})
	}

	sort.Slice(proposals, func(i, j int) bool {
		return proposals[i].Block < proposals[j].Block
	})

	if len(proposals) > limit {
		proposals = proposals[len(proposals)-limit:]
	}

	return proposals
}

func (s *Server) loadRecentVotes(ctx context.Context, latestHeight uint64, limit int) []messages.Vote {
	if limit <= 0 || s.storage == nil {
		return nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var minHeight uint64
	if latestHeight >= uint64(limit) && latestHeight > 0 {
		minHeight = latestHeight - uint64(limit) + 1
	}

	records, err := s.storage.ListVotes(queryCtx, minHeight, limit)
	if err != nil {
		return nil
	}

	items := make([]messages.Vote, 0, len(records))
	for _, rec := range records {
		var vote messages.Vote
		if err := utils.CBORUnmarshal(rec.Data, &vote); err != nil {
			continue
		}
		items = append(items, vote)
	}

	return items
}

func (s *Server) loadRecentQCs(ctx context.Context, latestHeight uint64, limit int) []messages.QC {
	if limit <= 0 || s.storage == nil {
		return nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var minHeight uint64
	if latestHeight >= uint64(limit) && latestHeight > 0 {
		minHeight = latestHeight - uint64(limit) + 1
	}

	records, err := s.storage.ListQCs(queryCtx, minHeight, limit)
	if err != nil {
		return nil
	}

	items := make([]messages.QC, 0, len(records))
	for _, rec := range records {
		var qc messages.QC
		if err := utils.CBORUnmarshal(rec.Data, &qc); err != nil {
			continue
		}
		items = append(items, qc)
	}

	return items
}

func (s *Server) buildConsensusVoteTimeline(
	ctx context.Context,
	now time.Time,
	latestHeight uint64,
	limit int,
	status consapi.EngineStatus,
	proposals []ConsensusProposalDTO,
) []ConsensusVoteDTO {
	type bucketCounts struct {
		proposals   uint64
		votes       uint64
		commits     uint64
		viewChanges uint64
	}

	buckets := make(map[int64]*bucketCounts)
	bucketMs := int64(consensusTimelineBucket / time.Millisecond)
	if bucketMs <= 0 {
		bucketMs = int64((30 * time.Second) / time.Millisecond)
	}

	bucketTimestamp := func(ts int64) int64 {
		if ts <= 0 {
			return ts
		}
		return (ts / bucketMs) * bucketMs
	}

	ensureBucket := func(ts int64) *bucketCounts {
		bucket := buckets[ts]
		if bucket == nil {
			bucket = &bucketCounts{}
			buckets[ts] = bucket
		}
		return bucket
	}

	for _, proposal := range proposals {
		ts := bucketTimestamp(proposal.Timestamp)
		ensureBucket(ts).proposals++
	}

	for _, vote := range s.loadRecentVotes(ctx, latestHeight, limit*3) {
		ts := bucketTimestamp(vote.Timestamp.UnixMilli())
		ensureBucket(ts).votes++
	}

	for _, qc := range s.loadRecentQCs(ctx, latestHeight, limit*2) {
		ts := bucketTimestamp(qc.Timestamp.UnixMilli())
		ensureBucket(ts).commits++
	}

	if vc := status.Metrics.ViewChanges; vc > 0 {
		ts := bucketTimestamp(now.UnixMilli())
		ensureBucket(ts).viewChanges = vc
	}

	if len(buckets) == 0 {
		ts := bucketTimestamp(now.UnixMilli())
		ensureBucket(ts)
	}

	timestamps := make([]int64, 0, len(buckets))
	for ts := range buckets {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	if len(timestamps) > limit {
		timestamps = timestamps[len(timestamps)-limit:]
	}

	result := make([]ConsensusVoteDTO, 0, len(timestamps)*4)
	for _, ts := range timestamps {
		bucket := buckets[ts]
		result = append(result, ConsensusVoteDTO{Type: "proposal", Count: bucket.proposals, Timestamp: ts})
		result = append(result, ConsensusVoteDTO{Type: "vote", Count: bucket.votes, Timestamp: ts})
		result = append(result, ConsensusVoteDTO{Type: "commit", Count: bucket.commits, Timestamp: ts})
		result = append(result, ConsensusVoteDTO{Type: "view_change", Count: bucket.viewChanges, Timestamp: ts})
	}

	return result
}

func (s *Server) buildSuspiciousNodes(ctx context.Context, validators []types.ValidatorInfo, now time.Time) []SuspiciousNodeDTO {
	heuristic := buildHeuristicSuspiciousNodes(validators, now)

	aiNodes, err := s.fetchAISuspiciousNodes(ctx)
	if err != nil && s.logger != nil {
		s.logger.Warn("failed to fetch AI suspicious nodes",
			utils.ZapError(err))
	}

	return mergeSuspiciousNodes(aiNodes, heuristic)
}

func buildHeuristicSuspiciousNodes(validators []types.ValidatorInfo, now time.Time) []SuspiciousNodeDTO {
	nodes := make([]SuspiciousNodeDTO, 0, len(validators))
	for _, v := range validators {
		uptime := reputationToUptime(v.Reputation)
		score, reason := computeSuspicionScore(v, uptime, now)
		if score < consensusSuspicionCutoff {
			continue
		}

		status := "warning"
		if score >= 70 {
			status = "critical"
		}

		nodes = append(nodes, SuspiciousNodeDTO{
			ID:             encodeValidatorID(v.ID),
			Status:         status,
			Uptime:         uptime,
			SuspicionScore: clampFloat(score, 0, 100),
			Reason:         reason,
		})
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].SuspicionScore == nodes[j].SuspicionScore {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].SuspicionScore > nodes[j].SuspicionScore
	})

	if len(nodes) > 10 {
		nodes = nodes[:10]
	}

	return nodes
}

func computeSuspicionScore(info types.ValidatorInfo, uptime float64, now time.Time) (float64, string) {
	reasons := make([]string, 0, 3)
	score := 0.0

	if !info.IsActive {
		score = math.Max(score, 70)
		reasons = append(reasons, "inactive")
	}

	if !info.LastSeen.IsZero() {
		staleness := now.Sub(info.LastSeen)
		switch {
		case staleness >= suspiciousStaleDuration2:
			score = math.Max(score, 85)
			reasons = append(reasons, "stale_gt_5m")
		case staleness >= suspiciousStaleDuration1:
			score = math.Max(score, 60)
			reasons = append(reasons, "stale_gt_2m")
		}
	}

	if deficit := 100 - uptime; deficit > score {
		score = deficit
		reasons = append(reasons, "low_uptime")
	} else if deficit >= consensusSuspicionCutoff {
		score = math.Max(score, deficit)
		reasons = append(reasons, "low_uptime")
	}

	reason := strings.Join(reasons, ",")
	return score, reason
}

func computeQuorumSize(total int) int {
	if total <= 0 {
		return 0
	}
	if total == 1 {
		return 1
	}

	f := (total - 1) / 3
	if f < 1 {
		f = 1
	}
	quorum := 2*f + 1
	if quorum > total {
		quorum = total
	}
	return quorum
}

type aiSuspiciousNodesResponse struct {
	Nodes []struct {
		ID             string   `json:"id"`
		Status         string   `json:"status"`
		Uptime         float64  `json:"uptime"`
		SuspicionScore float64  `json:"suspicion_score"`
		Reason         string   `json:"reason"`
		ThreatTypes    []string `json:"threat_types"`
	} `json:"nodes"`
}

func (s *Server) fetchAISuspiciousNodes(ctx context.Context) ([]SuspiciousNodeDTO, error) {
	if s.aiClient == nil || s.aiBaseURL == "" {
		return nil, nil
	}

	endpoint := strings.TrimRight(s.aiBaseURL, "/") + "/detections/suspicious-nodes?limit=25"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build AI request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if s.aiAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.aiAuthToken)
	}

	resp, err := s.aiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call AI service: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("ai service returned status %d", resp.StatusCode)
	}

	var payload aiSuspiciousNodesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode AI response: %w", err)
	}

	nodes := make([]SuspiciousNodeDTO, 0, len(payload.Nodes))
	for _, node := range payload.Nodes {
		dto := SuspiciousNodeDTO{
			ID:             node.ID,
			Status:         node.Status,
			Uptime:         clampFloat(node.Uptime, 0, 100),
			SuspicionScore: clampFloat(node.SuspicionScore, 0, 100),
			Reason:         node.Reason,
		}

		if dto.Status == "" {
			if dto.SuspicionScore >= 70 {
				dto.Status = "critical"
			} else if dto.SuspicionScore >= 40 {
				dto.Status = "warning"
			} else {
				dto.Status = "healthy"
			}
		}

		nodes = append(nodes, dto)
	}

	return nodes, nil
}

func mergeSuspiciousNodes(aiNodes, heuristic []SuspiciousNodeDTO) []SuspiciousNodeDTO {
	if len(aiNodes) == 0 && len(heuristic) == 0 {
		return nil
	}

	combined := make([]SuspiciousNodeDTO, 0, len(aiNodes)+len(heuristic))
	seen := make(map[string]struct{})

	for _, node := range aiNodes {
		if node.ID != "" {
			seen[node.ID] = struct{}{}
		}
		combined = append(combined, node)
	}

	for _, node := range heuristic {
		if _, exists := seen[node.ID]; exists {
			continue
		}
		combined = append(combined, node)
	}

	sort.Slice(combined, func(i, j int) bool {
		if combined[i].SuspicionScore == combined[j].SuspicionScore {
			return combined[i].ID < combined[j].ID
		}
		return combined[i].SuspicionScore > combined[j].SuspicionScore
	})

	if len(combined) > 10 {
		combined = combined[:10]
	}

	return combined
}
