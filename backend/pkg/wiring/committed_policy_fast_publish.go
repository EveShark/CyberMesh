package wiring

import (
	"context"
	"os"
	"strings"
	"time"

	"backend/pkg/block"
	"backend/pkg/utils"
)

func committedPolicyPayloads(ab *block.AppBlock) [][]byte {
	return extractCommitMetadata(ab, nil).policyPayloads
}

func committedPolicies(ab *block.AppBlock) []committedPolicyPayload {
	return extractCommitMetadata(ab, nil).policies
}

func (s *Service) publishCommittedPoliciesAfterStateApply(ctx context.Context, ab *block.AppBlock) {
	if s == nil || ab == nil || !s.fastPublishAfterApplyEnabled() || s.policyPublisher == nil {
		return
	}
	if !s.shouldFastPublishCommittedBlock(ab) {
		return
	}
	policies := committedPolicies(ab)
	if len(policies) == 0 {
		return
	}

	timeout := s.policyFastPublishTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	baseCtx := context.Background()
	go func() {
		publishCtx, cancel := context.WithTimeout(baseCtx, timeout)
		defer cancel()
		if s.log != nil {
			s.log.InfoContext(ctx, "fast publishing committed policies after state apply",
				utils.ZapUint64("height", ab.GetHeight()),
				utils.ZapInt("policy_count", len(policies)))
		}
		results := s.policyPublisher.PublishCommittedFastAfterCommit(publishCtx, policies)
		if s.log != nil {
			failures := 0
			for _, result := range results {
				if result.Err != nil {
					failures++
				}
			}
			s.log.InfoContext(ctx, "fast committed policy publish completed",
				utils.ZapUint64("height", ab.GetHeight()),
				utils.ZapInt("policy_count", len(results)),
				utils.ZapInt("failures", failures))
		}
	}()
}

func (s *Service) shouldFastPublishCommittedBlock(ab *block.AppBlock) bool {
	if s == nil || ab == nil || s.policyPublisher == nil || !s.policyPublishOnCommitEnabled() {
		return false
	}
	if !s.policyCommitProposerOnly {
		return true
	}
	if s.eng != nil {
		status := s.eng.GetStatus()
		if status.NodeID == ab.Proposer() {
			return true
		}
	}
	localNodeID := ""
	consensusNodes := ""
	if s.cfg.ConfigManager != nil {
		localNodeID = strings.TrimSpace(s.cfg.ConfigManager.GetString("NODE_ID", ""))
		consensusNodes = strings.TrimSpace(s.cfg.ConfigManager.GetString("CONSENSUS_NODES", ""))
	}
	if isDeterministicFastPublishOwner(localNodeID, consensusNodes, ab.GetHeight()) {
		if s.log != nil {
			s.log.DebugContext(context.Background(), "fast publish using deterministic owner fallback",
				utils.ZapUint64("height", ab.GetHeight()),
				utils.ZapString("node_id", localNodeID))
		}
		return true
	}
	if s.log != nil {
		s.log.DebugContext(context.Background(), "Skipping fast policy publish on non-owner validator",
			utils.ZapUint64("height", ab.GetHeight()))
	}
	return false
}

func (s *Service) fastPublishAfterApplyEnabled() bool {
	if s != nil && s.policyFastPublishAfterApply {
		return true
	}
	return envBool("CONTROL_POLICY_FAST_PUBLISH_AFTER_APPLY")
}

func (s *Service) policyPublishOnCommitEnabled() bool {
	if s != nil && s.policyPublishOnCommit {
		return true
	}
	source := strings.ToLower(strings.TrimSpace(os.Getenv("CONTROL_POLICY_PUBLISH_SOURCE")))
	return source == "" || source == "both" || source == "commit"
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "t", "yes", "y", "on", "enabled":
		return true
	default:
		return false
	}
}

func isDeterministicFastPublishOwner(localNodeID, consensusNodes string, height uint64) bool {
	localNodeID = strings.TrimSpace(localNodeID)
	consensusNodes = strings.TrimSpace(consensusNodes)
	if localNodeID == "" || consensusNodes == "" || height == 0 {
		return false
	}
	parts := strings.Split(consensusNodes, ",")
	nodes := make([]string, 0, len(parts))
	for _, part := range parts {
		node := strings.TrimSpace(part)
		if node != "" {
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return false
	}
	owner := nodes[int((height-1)%uint64(len(nodes)))]
	return localNodeID == owner
}
