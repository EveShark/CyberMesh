package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/ack"
	"github.com/CyberMesh/enforcement-agent/internal/control"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
	"github.com/CyberMesh/enforcement-agent/internal/ratelimit"
	"github.com/CyberMesh/enforcement-agent/internal/state"

	pb "backend/proto"
	"google.golang.org/protobuf/proto"
)

// Controller bridges Kafka messages to enforcement backend.
type Controller struct {
	trust             *policy.TrustedKeys
	store             *state.Store
	enforcer          enforcer.Enforcer
	metrics           *metrics.Recorder
	logger            *zap.Logger
	rate              ratelimit.Coordinator
	killSwitch        *control.KillSwitch
	seenHashes        sync.Map // policyID -> string(hash)
	pending           map[string]policy.Event
	pendingMu         sync.Mutex
	fastPath          FastPathConfig
	acks              ack.Publisher
	ackEnqueueTimeout time.Duration
	instanceID        string
	replay            replayConfig
	replaySeen        map[string]time.Time
	replayMu          sync.Mutex
	scopeLocks        sync.Map // scope key -> *sync.Mutex
	decisionMu        sync.RWMutex
	decisions         []DecisionRecord
	decMax            int
}

type DecisionRecord struct {
	PolicyID       string    `json:"policy_id"`
	Action         string    `json:"action"`
	Scope          string    `json:"scope"`
	Tenant         string    `json:"tenant,omitempty"`
	Result         string    `json:"result"`
	ErrorCode      string    `json:"error_code,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	AppliedAt      time.Time `json:"applied_at,omitempty"`
	AckEnqueueAt   time.Time `json:"ack_enqueue_at,omitempty"`
	AckJournaledAt time.Time `json:"ack_journaled_at,omitempty"`
	AckedAt        time.Time `json:"acked_at,omitempty"`
	AckRequired    bool      `json:"ack_required"`
	AckAttempted   bool      `json:"ack_attempted"`
	AckStatus      string    `json:"ack_status"`
}

type FastPathConfig struct {
	Enabled         bool
	MinConfidence   float64
	SignalsRequired int64
}

type Options struct {
	FastPathEnabled       bool
	FastPathMinConfidence float64
	FastPathSignals       int64
	AckPublisher          ack.Publisher
	AckEnqueueTimeout     time.Duration
	ControllerInstanceID  string
	ReplayWindow          time.Duration
	ReplayFutureSkew      time.Duration
	ReplayCacheMaxEntries int
}

type replayConfig struct {
	window         time.Duration
	futureSkew     time.Duration
	cacheMax       int
	enabled        bool
	requirePayload bool
}

const (
	namespaceScopePrefix      = "namespace:"
	nodeScopePrefix           = "node:"
	clusterScopeKey           = "cluster"
	globalScopeKey            = "global"
	maxFastMitigationRenewals = 1
	stageEnforcementConsume   = "t_enforcement_consume"
	stageEnforcementApplyDone = "t_enforcement_apply_done"
	stageEnforcementPersisted = "t_enforcement_persist_done"
	stageAckPublishStart      = "t_ack_publish_start"
	stageAckPublishDone       = "t_ack_publish_done"
)

// New constructs a controller.
func New(trust *policy.TrustedKeys, store *state.Store, backend enforcer.Enforcer, recorder *metrics.Recorder, rate ratelimit.Coordinator, kill *control.KillSwitch, logger *zap.Logger, opts Options) *Controller {
	replayWindow := opts.ReplayWindow
	if replayWindow <= 0 {
		replayWindow = 10 * time.Minute
	}
	replayFutureSkew := opts.ReplayFutureSkew
	if replayFutureSkew <= 0 {
		replayFutureSkew = 30 * time.Second
	}
	replayMax := opts.ReplayCacheMaxEntries
	if replayMax <= 0 {
		replayMax = 20000
	}
	c := &Controller{
		trust:      trust,
		store:      store,
		enforcer:   backend,
		metrics:    recorder,
		rate:       rate,
		killSwitch: kill,
		logger:     logger,
		pending:    make(map[string]policy.Event),
		fastPath: FastPathConfig{
			Enabled:         opts.FastPathEnabled,
			MinConfidence:   opts.FastPathMinConfidence,
			SignalsRequired: opts.FastPathSignals,
		},
		acks:              opts.AckPublisher,
		ackEnqueueTimeout: opts.AckEnqueueTimeout,
		instanceID:        opts.ControllerInstanceID,
		replay: replayConfig{
			window:         replayWindow,
			futureSkew:     replayFutureSkew,
			cacheMax:       replayMax,
			enabled:        replayWindow > 0,
			requirePayload: true,
		},
		replaySeen: make(map[string]time.Time, replayMax),
		decMax:     500,
	}
	if c.ackEnqueueTimeout <= 0 {
		c.ackEnqueueTimeout = 500 * time.Millisecond
	}
	c.loadPendingApprovals()
	return c
}

func (c *Controller) loadPendingApprovals() {
	if c.store == nil || c.trust == nil {
		return
	}
	for _, key := range c.store.PendingApprovalKeys() {
		payload, ok := c.store.PendingApproval(key)
		if !ok {
			continue
		}
		var evt pb.PolicyUpdateEvent
		if err := proto.Unmarshal(payload, &evt); err != nil {
			if c.logger != nil {
				c.logger.Warn("failed to unmarshal pending approval", zap.String("key", key), zap.Error(err))
			}
			continue
		}
		parsed, err := c.trust.VerifyAndParse(&evt)
		if err != nil {
			if c.logger != nil {
				c.logger.Warn("pending approval failed verification", zap.String("key", key), zap.Error(err))
			}
			continue
		}
		c.pendingMu.Lock()
		c.pending[key] = parsed
		c.pendingMu.Unlock()
	}
}

// HandleMessage satisfies kafka.MessageHandler.
func (c *Controller) HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	consumeStart := time.Now()
	c.metrics.ObserveIngest()
	if c.logger != nil {
		c.logger.Debug(
			"policy message received",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", msg.Partition),
			zap.Int64("offset", msg.Offset),
			zap.Int("value_bytes", len(msg.Value)),
			zap.Int("key_bytes", len(msg.Key)),
		)
	}

	var event pb.PolicyUpdateEvent
	if err := proto.Unmarshal(msg.Value, &event); err != nil {
		c.metrics.ObserveRejected()
		if c.logger != nil {
			c.logger.Warn("failed to unmarshal policy", zap.Error(err), zap.Int("value_bytes", len(msg.Value)))
		}
		return nil
	}

	evt, err := c.trust.VerifyAndParse(&event)
	if err != nil {
		c.metrics.ObserveRejected()
		if c.logger != nil {
			c.logger.Warn("policy verification failed", zap.String("policy_id", event.PolicyId), zap.Error(err))
		}
		return nil
	}
	c.emitStageMarker(evt, stageEnforcementConsume, time.Now().UnixMilli())
	if c.logger != nil {
		c.logger.Debug(
			"policy verification passed",
			zap.String("policy_id", evt.Spec.ID),
			zap.String("action", evt.Spec.Action),
			zap.String("scope", scopeIdentifier(evt.Spec)),
		)
	}

	c.metrics.ObserveValidated()
	requiresAck := evt.Proto.GetRequiresAck()
	appliedAt := time.Time{}

	fastPath := EvaluateFastPath(evt.Spec, c.fastPath)
	if c.metrics != nil {
		if fastPath.Eligible {
			c.metrics.ObserveFastPathEligibility("eligible")
		} else {
			c.metrics.ObserveFastPathEligibility(fastPath.Reason)
		}
	}
	if c.logger != nil {
		c.logger.Debug("fast-path evaluated",
			zap.String("policy_id", evt.Spec.ID),
			zap.Bool("eligible", fastPath.Eligible),
			zap.String("reason", fastPath.Reason))
	}

	replayNow := time.Now().UTC()
	replayReason, replayKey, replayErr := c.checkReplay(evt, replayNow)
	if replayErr != nil {
		if c.metrics != nil {
			c.metrics.ObserveGuardrailViolation(replayReason)
			c.metrics.ObserveGatewayReplayReject(replayReason)
		}
		if c.logger != nil {
			c.logger.Warn("policy replay guard rejected event",
				zap.String("policy_id", evt.Spec.ID),
				zap.String("reason", replayReason),
				zap.Error(replayErr))
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, replayReason, replayErr.Error(), appliedAt)
		return nil
	}

	if c.killSwitch != nil && c.killSwitch.Enabled() {
		if c.logger != nil {
			c.logger.Warn("kill switch active, ignoring policy", zap.String("policy_id", evt.Spec.ID))
		}
		if c.metrics != nil {
			c.metrics.ObserveGuardrailViolation("kill_switch")
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "kill_switch", "enforcement disabled via kill switch", appliedAt)
		return nil
	}

	scopeKey := orderingKey(evt)
	eventTS, hasEventTS := policyEventTimestamp(evt)
	advanceScopeWatermark := func() {}
	if c.store != nil && hasEventTS {
		unlock := c.lockScope(scopeKey)
		defer unlock()
		advanceScopeWatermark = func() {
			c.store.AdvanceScopeWatermark(scopeKey, eventTS)
		}
		if stale, lastAccepted := c.store.IsScopeEventStale(scopeKey, eventTS); stale {
			if c.metrics != nil {
				c.metrics.ObserveGuardrailViolation("stale_out_of_order")
			}
			if c.logger != nil {
				c.logger.Warn("policy stale out-of-order event dropped",
					zap.String("policy_id", evt.Spec.ID),
					zap.String("ordering_key", scopeKey),
					zap.Time("event_ts", eventTS),
					zap.Time("last_accepted_ts", lastAccepted))
			}
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "stale_out_of_order", "policy event timestamp older than accepted scope watermark", appliedAt)
			return nil
		}
	}

	pendingKey := approvalKey(evt.Spec)
	requiresApproval := evt.Spec.Guardrails.ApprovalRequired
	approvedFromPending := false
	rejectedFromPending := false
	if isApprovalAction(evt.Proto.GetAction()) {
		c.pendingMu.Lock()
		pendingEvt, ok := c.pending[pendingKey]
		c.pendingMu.Unlock()
		if ok {
			evt = mergePendingDecisionContext(pendingEvt, evt)
			requiresApproval = false
			evt.Spec.Guardrails.ApprovalRequired = false
			if evt.Proto != nil {
				evt.Proto.RequiresAck = false
			}
			approvedFromPending = true
		} else {
			if c.logger != nil {
				c.logger.Warn("approval received without pending policy", zap.String("policy_id", evt.Spec.ID), zap.String("scope", rateScope(evt.Spec)))
			}
			appliedAt = time.Now().UTC()
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "approval_not_pending", "approval received without pending policy", appliedAt)
			return nil
		}
	}
	if isRejectAction(evt.Proto.GetAction()) {
		c.pendingMu.Lock()
		pendingEvt, ok := c.pending[pendingKey]
		c.pendingMu.Unlock()
		if ok {
			evt = mergePendingDecisionContext(pendingEvt, evt)
			requiresApproval = false
			evt.Spec.Guardrails.ApprovalRequired = false
			if evt.Proto != nil {
				evt.Proto.RequiresAck = false
			}
			rejectedFromPending = true
		} else {
			if c.logger != nil {
				c.logger.Warn("rejection received without pending policy", zap.String("policy_id", evt.Spec.ID), zap.String("scope", rateScope(evt.Spec)))
			}
			appliedAt = time.Now().UTC()
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "reject_not_pending", "rejection received without pending policy", appliedAt)
			return nil
		}
	}

	if requiresApproval {
		if err := c.savePendingApproval(pendingKey, evt); err != nil && c.logger != nil {
			c.logger.Error("failed to persist pending approval", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
		}
		c.pendingMu.Lock()
		c.pending[pendingKey] = evt
		c.pendingMu.Unlock()
		c.commitReplayKey(replayKey, time.Now().UTC())
		if c.logger != nil {
			c.logger.Info("policy pending manual approval", zap.String("policy_id", evt.Spec.ID), zap.String("scope", rateScope(evt.Spec)))
		}
		return nil
	}
	if rejectedFromPending {
		appliedAt = time.Now().UTC()
		c.clearPendingApproval(pendingKey)
		advanceScopeWatermark()
		c.commitDuplicateFingerprint(evt)
		c.commitReplayKey(replayKey, time.Now().UTC())
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "manual_reject", rejectDecisionReason(evt.Spec), appliedAt)
		if c.logger != nil {
			c.logger.Info("policy manually rejected", zap.String("policy_id", evt.Spec.ID), zap.String("scope", rateScope(evt.Spec)))
		}
		return nil
	}

	if c.isDuplicate(evt) {
		if c.logger != nil {
			c.logger.Debug("duplicate policy event treated as idempotent noop", zap.String("policy_id", evt.Spec.ID))
		}
		appliedAt = time.Now().UTC()
		c.emitStageMarker(evt, stageEnforcementApplyDone, appliedAt.UnixMilli(), zap.String("result", "duplicate_noop"))
		if c.metrics != nil {
			c.metrics.ObserveConsumeToApply("duplicate", time.Since(consumeStart))
			c.metrics.ObserveDuplicateScope(scopeKindLabel(evt.Spec))
		}
		advanceScopeWatermark()
		c.commitDuplicateFingerprint(evt)
		c.commitReplayKey(replayKey, time.Now().UTC())
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "duplicate_noop", "policy duplicate detected", appliedAt)
		return nil
	}

	if isRemoveAction(evt) {
		removeReason := ""
		if c.store != nil {
			if existing, ok := c.store.Get(evt.Spec.ID); ok && existing.PendingConsensus {
				removeReason = "provisional_revoked"
			}
		}
		start := time.Now()
		if c.logger != nil {
			c.logger.Debug(
				"policy remove started",
				zap.String("policy_id", evt.Spec.ID),
				zap.String("rule_type", evt.Spec.RuleType),
			)
		}
		if err := c.enforcer.Remove(ctx, evt.Spec.ID); err != nil {
			if c.metrics != nil {
				c.metrics.ObserveApplyError(time.Since(start))
				c.metrics.ObserveConsumeToApply("error", time.Since(consumeStart))
			}
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "remove_error", err.Error(), time.Now().UTC())
			return fmt.Errorf("controller: remove policy %s: %w", evt.Spec.ID, err)
		}
		appliedAt = time.Now().UTC()
		c.emitStageMarker(evt, stageEnforcementApplyDone, appliedAt.UnixMilli(), zap.String("result", "remove"))
		if c.metrics != nil {
			c.metrics.ObserveApplied(time.Since(start))
			c.metrics.ObserveConsumeToApply("success", time.Since(consumeStart))
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "", removeReason, appliedAt)
		if c.store != nil {
			if err := c.store.Remove(evt.Spec.ID); err != nil && c.logger != nil {
				c.logger.Warn("failed to remove policy state", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
			} else if c.metrics != nil {
				c.metrics.SetActivePolicies(c.store.ActiveCount())
			}
		}
		advanceScopeWatermark()
		c.commitDuplicateFingerprint(evt)
		c.commitReplayKey(replayKey, time.Now().UTC())
		if approvedFromPending {
			c.clearPendingApproval(pendingKey)
		}
		if c.logger != nil {
			c.logger.Debug("policy remove succeeded", zap.String("policy_id", evt.Spec.ID), zap.Duration("latency", time.Since(start)))
		}
		return nil
	}

	reservation, reason, err := c.checkGuardrails(ctx, evt.Spec)
	if err != nil {
		if reservation != nil {
			_ = reservation.Release(ctx)
		}
		if c.metrics != nil {
			c.metrics.ObserveGuardrailViolation(reason)
		}
		if c.logger != nil {
			c.logger.Warn("policy guardrail violation",
				zap.String("policy_id", evt.Spec.ID),
				zap.String("reason", reason),
				zap.Error(err))
		}
		errorCode := "guardrail_violation"
		if errors.Is(err, ratelimit.ErrRateLimitExceeded) {
			errorCode = "rate_limit_exceeded"
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, errorCode, err.Error(), appliedAt)
		return nil
	}

	refreshReason := "applied_new"
	refreshOnly := false
	promotedFromFast := false
	if c.store != nil {
		if existing, ok := c.store.Get(evt.Spec.ID); ok {
			promotedFromFast = existing.PendingConsensus && existing.Provisional
			if sameEffectiveRule(existing.Spec, evt.Spec) {
				refreshOnly = true
				if promotedFromFast {
					refreshReason = "applied_promoted"
				} else {
					refreshReason = "applied_refresh"
				}
			} else {
				if promotedFromFast {
					refreshReason = "applied_promoted_reapply"
				} else {
					refreshReason = "applied_refresh_reapply"
				}
			}
		}
	}

	if refreshOnly {
		appliedAt = time.Now().UTC()
		c.emitStageMarker(evt, stageEnforcementApplyDone, appliedAt.UnixMilli(), zap.String("result", "refresh_only"))
		persistStart := time.Now()
		if err := c.store.Upsert(evt.Spec, appliedAt); err != nil {
			if c.metrics != nil {
				c.metrics.ObserveApplyPersist("error", time.Since(persistStart))
				c.metrics.ObserveConsumeToApply("error", time.Since(consumeStart))
			}
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "refresh_persist_error", err.Error(), appliedAt)
			return fmt.Errorf("controller: refresh policy %s: %w", evt.Spec.ID, err)
		}
		if reservation != nil {
			if err := reservation.Commit(ctx); err != nil && c.logger != nil {
				c.logger.Warn("rate reservation commit failed", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
			}
		}
		if c.metrics != nil {
			c.metrics.ObserveApplyPersist("success", time.Since(persistStart))
			c.metrics.ObserveConsumeToApply("success", time.Since(consumeStart))
			c.metrics.SetActivePolicies(c.store.ActiveCount())
		}
		c.emitStageMarker(evt, stageEnforcementPersisted, time.Now().UnixMilli())
		if c.store != nil {
			if promotedFromFast {
				_ = c.store.MarkPromoted(evt.Spec.ID, appliedAt)
			} else {
				_ = c.store.ClearPendingConsensus(evt.Spec.ID)
			}
		}
		advanceScopeWatermark()
		c.commitDuplicateFingerprint(evt)
		c.commitReplayKey(replayKey, time.Now().UTC())
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "", refreshReason, appliedAt)
		if approvedFromPending {
			c.clearPendingApproval(pendingKey)
		}
		if c.logger != nil {
			c.logger.Debug("policy refresh applied without re-enforce", zap.String("policy_id", evt.Spec.ID))
		}
		return nil
	}

	start := time.Now()
	if c.logger != nil {
		c.logger.Debug(
			"policy apply started",
			zap.String("policy_id", evt.Spec.ID),
			zap.String("action", evt.Spec.Action),
			zap.String("rule_type", evt.Spec.RuleType),
			zap.Bool("dry_run", evt.Spec.Guardrails.DryRun),
		)
	}
	if err := c.enforcer.Apply(ctx, evt.Spec); err != nil {
		if reservation != nil {
			_ = reservation.Release(ctx)
		}
		c.metrics.ObserveApplyError(time.Since(start))
		if c.metrics != nil {
			c.metrics.ObserveConsumeToApply("error", time.Since(consumeStart))
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "apply_error", err.Error(), time.Now().UTC())
		return fmt.Errorf("controller: apply policy %s: %w", evt.Spec.ID, err)
	}
	appliedAt = time.Now().UTC()
	c.metrics.ObserveApplied(time.Since(start))
	c.emitStageMarker(evt, stageEnforcementApplyDone, appliedAt.UnixMilli())
	if c.metrics != nil {
		c.metrics.ObserveConsumeToApply("success", time.Since(consumeStart))
	}
	if c.logger != nil {
		c.logger.Debug("policy apply succeeded", zap.String("policy_id", evt.Spec.ID), zap.Duration("latency", time.Since(start)))
	}
	c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "", refreshReason, appliedAt)

	if reservation != nil {
		if err := reservation.Commit(ctx); err != nil && c.logger != nil {
			c.logger.Warn("rate reservation commit failed", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
		}
	}

	persistStart := time.Now()
	if err := c.store.Upsert(evt.Spec, time.Now().UTC()); err != nil {
		if c.metrics != nil {
			c.metrics.ObserveApplyPersist("error", time.Since(persistStart))
		}
		if c.logger != nil {
			c.logger.Error("failed to persist policy state", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
		}
	} else if c.metrics != nil {
		c.metrics.ObserveApplyPersist("success", time.Since(persistStart))
		c.metrics.SetActivePolicies(c.store.ActiveCount())
	}
	c.emitStageMarker(evt, stageEnforcementPersisted, time.Now().UnixMilli())
	if c.store != nil {
		if promotedFromFast {
			_ = c.store.MarkPromoted(evt.Spec.ID, appliedAt)
		} else {
			_ = c.store.ClearPendingConsensus(evt.Spec.ID)
		}
	}
	advanceScopeWatermark()
	c.commitDuplicateFingerprint(evt)
	c.commitReplayKey(replayKey, time.Now().UTC())

	if approvedFromPending {
		c.clearPendingApproval(pendingKey)
	}

	if c.logger != nil {
		c.logger.Debug("policy applied",
			zap.String("policy_id", evt.Spec.ID),
			zap.String("rule_type", evt.Spec.RuleType),
			zap.Bool("dry_run", evt.Spec.Guardrails.DryRun),
			zap.String("action", evt.Spec.Action),
			zap.Int("target_ips", len(evt.Spec.Target.IPs)),
			zap.Int("target_cidrs", len(evt.Spec.Target.CIDRs)),
			zap.String("scope", evt.Spec.Target.Scope),
		)
	}

	return nil
}

func sameEffectiveRule(a policy.PolicySpec, b policy.PolicySpec) bool {
	return effectiveRuleFingerprint(a) == effectiveRuleFingerprint(b)
}

func effectiveRuleFingerprint(spec policy.PolicySpec) string {
	target := map[string]any{
		"ips":       append([]string(nil), spec.Target.IPs...),
		"cidrs":     append([]string(nil), spec.Target.CIDRs...),
		"direction": strings.ToLower(strings.TrimSpace(spec.Target.Direction)),
		"scope":     strings.ToLower(strings.TrimSpace(spec.Target.Scope)),
		"selectors": cloneStringMap(spec.Target.Selectors),
		"namespace": strings.ToLower(strings.TrimSpace(spec.Target.Namespace)),
		"ports":     normalizePortRanges(spec.Target.Ports),
		"protocols": normalizeStrings(spec.Target.Protocols),
		"tenant":    strings.ToLower(strings.TrimSpace(spec.Target.Tenant)),
		"region":    strings.ToLower(strings.TrimSpace(spec.Target.Region)),
	}
	guardrails := map[string]any{
		"dry_run":                spec.Guardrails.DryRun,
		"canary_scope":           spec.Guardrails.CanaryScope,
		"cidr_max_prefix_len":    valueOrNil(spec.Guardrails.CIDRMaxPrefixLen),
		"max_targets":            valueOrNil(spec.Guardrails.MaxTargets),
		"fast_path_enabled":      spec.Guardrails.FastPathEnabled,
		"fast_path_canary_scope": spec.Guardrails.FastPathCanaryScope,
		"allowlist_ips":          normalizeStrings(spec.Guardrails.AllowlistIPs),
		"allowlist_cidrs":        normalizeStrings(spec.Guardrails.AllowlistCIDRs),
		"allowlist_namespaces":   normalizeStrings(spec.Guardrails.AllowlistNamespaces),
		"approval_required":      spec.Guardrails.ApprovalRequired,
	}
	criteria := map[string]any{
		"min_confidence":      floatValueOrNil(spec.Criteria.MinConfidence),
		"attempts_per_window": valueOrNil(spec.Criteria.AttemptsPerWindow),
		"window_seconds":      valueOrNil(spec.Criteria.WindowSeconds),
	}
	payload := map[string]any{
		"id":       strings.TrimSpace(spec.ID),
		"ruleType": strings.ToLower(strings.TrimSpace(spec.RuleType)),
		"action":   strings.ToLower(strings.TrimSpace(spec.Action)),
		"tenant":   strings.ToLower(strings.TrimSpace(spec.Tenant)),
		"region":   strings.ToLower(strings.TrimSpace(spec.Region)),
		"target":   target,
		"criteria": criteria,
		"guards":   guardrails,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		// Selector keys are canonicalized, but values are preserved (trimmed only)
		// because some selector backends can treat value case as semantic input.
		out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return out
}

func normalizeStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		token := strings.ToLower(strings.TrimSpace(item))
		if token != "" {
			out = append(out, token)
		}
	}
	sort.Strings(out)
	return out
}

func normalizePortRanges(in []policy.PortRange) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	for _, pr := range in {
		out = append(out, fmt.Sprintf("%d-%d", pr.From, pr.To))
	}
	sort.Strings(out)
	return out
}

func valueOrNil[T any](ptr *T) any {
	if ptr == nil {
		return nil
	}
	return *ptr
}

func floatValueOrNil(ptr *float64) any {
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (c *Controller) isDuplicate(evt policy.Event) bool {
	hash := duplicateFingerprint(evt)
	key := fmt.Sprintf("%s:%s", evt.Spec.ID, scopeIdentifier(evt.Spec))
	prev, ok := c.seenHashes.Load(key)
	return ok && prev.(string) == hash
}

func (c *Controller) commitDuplicateFingerprint(evt policy.Event) {
	if c == nil {
		return
	}
	hash := duplicateFingerprint(evt)
	key := fmt.Sprintf("%s:%s", evt.Spec.ID, scopeIdentifier(evt.Spec))
	c.seenHashes.Store(key, hash)
}

func duplicateFingerprint(evt policy.Event) string {
	if evt.Fast != nil {
		return strings.Join([]string{
			"fast",
			strings.TrimSpace(evt.Fast.MitigationID),
			strings.TrimSpace(evt.Fast.ContentHash),
		}, "|")
	}
	return fmt.Sprintf("%x", evtRuleHash(evt))
}

func (c *Controller) checkReplay(evt policy.Event, now time.Time) (string, string, error) {
	if !c.replay.enabled {
		return "", "", nil
	}
	if evt.Proto == nil {
		if c.replay.requirePayload {
			return "replay_missing_envelope", "", fmt.Errorf("missing policy envelope for replay validation")
		}
		return "", "", nil
	}

	eventTS := time.Unix(evt.Proto.GetTimestamp(), 0).UTC()
	if evt.Proto.GetTimestamp() <= 0 {
		return "replay_invalid_timestamp", "", fmt.Errorf("invalid timestamp in policy envelope")
	}
	if now.Add(c.replay.futureSkew).Before(eventTS) {
		return "replay_future_timestamp", "", fmt.Errorf("policy timestamp %s exceeds future skew %s", eventTS.Format(time.RFC3339), c.replay.futureSkew.String())
	}
	if eventTS.Before(now.Add(-c.replay.window)) {
		return "replay_stale_timestamp", "", fmt.Errorf("policy timestamp %s outside replay window %s", eventTS.Format(time.RFC3339), c.replay.window.String())
	}

	key := replayKey(evt)
	c.replayMu.Lock()
	defer c.replayMu.Unlock()
	c.pruneReplayLocked(now)

	if seenAt, ok := c.replaySeen[key]; ok {
		if now.Sub(seenAt) <= c.replay.window {
			return "replay_duplicate", key, fmt.Errorf("duplicate replay key within window")
		}
	}
	return "", key, nil
}

func (c *Controller) commitReplayKey(key string, now time.Time) {
	if c == nil || !c.replay.enabled {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	c.replayMu.Lock()
	defer c.replayMu.Unlock()
	c.pruneReplayLocked(now)
	c.replaySeen[key] = now
	if len(c.replaySeen) > c.replay.cacheMax {
		c.evictReplayLocked(len(c.replaySeen) - c.replay.cacheMax)
	}
}

func replayKey(evt policy.Event) string {
	policyID := evt.Spec.ID
	if evt.Proto != nil && strings.TrimSpace(evt.Proto.GetPolicyId()) != "" {
		policyID = strings.TrimSpace(evt.Proto.GetPolicyId())
	}
	return strings.Join([]string{
		policyID,
		hex.EncodeToString(evt.Proto.GetRuleHash()),
		hex.EncodeToString(evt.Proto.GetProducerId()),
		strconv.FormatInt(evt.Proto.GetTimestamp(), 10),
	}, "|")
}

func policyEventTimestamp(evt policy.Event) (time.Time, bool) {
	if evt.Proto == nil || evt.Proto.GetTimestamp() <= 0 {
		return time.Time{}, false
	}
	return time.Unix(evt.Proto.GetTimestamp(), 0).UTC(), true
}

func orderingKey(evt policy.Event) string {
	if evt.Proto != nil {
		if id := strings.TrimSpace(evt.Proto.GetPolicyId()); id != "" {
			return "policy:" + id
		}
	}
	if id := strings.TrimSpace(evt.Spec.ID); id != "" {
		return "policy:" + id
	}
	return "scope:" + scopeIdentifier(evt.Spec)
}

func (c *Controller) lockScope(scopeKey string) func() {
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" {
		scopeKey = globalScopeKey
	}
	muAny, _ := c.scopeLocks.LoadOrStore(scopeKey, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (c *Controller) pruneReplayLocked(now time.Time) {
	if !c.replay.enabled {
		return
	}
	cutoff := now.Add(-c.replay.window)
	for k, seen := range c.replaySeen {
		if seen.Before(cutoff) {
			delete(c.replaySeen, k)
		}
	}
}

func (c *Controller) evictReplayLocked(toRemove int) {
	if toRemove <= 0 {
		return
	}
	for k := range c.replaySeen {
		delete(c.replaySeen, k)
		toRemove--
		if toRemove == 0 {
			return
		}
	}
}

func (c *Controller) checkGuardrails(ctx context.Context, spec policy.PolicySpec) (ratelimit.Reservation, string, error) {
	guard := spec.Guardrails
	reservations := make([]ratelimit.Reservation, 0, 4)
	releaseAll := func() {
		for _, r := range reservations {
			if r != nil {
				_ = r.Release(ctx)
			}
		}
	}

	if reason, err := checkAllowlist(spec); err != nil {
		return nil, reason, err
	}

	if guard.EscalationCooldown != nil && c.store != nil {
		cooldown := time.Duration(*guard.EscalationCooldown) * time.Second
		if cooldown > 0 && c.store.LastAppliedWithin(spec.ID, cooldown, time.Now().UTC()) {
			return nil, "escalation_cooldown", fmt.Errorf("policy %s within escalation cooldown", spec.ID)
		}
	}

	if guard.MaxActivePolicies != nil {
		active := c.store.ActiveCount()
		if int64(active) >= *guard.MaxActivePolicies {
			return nil, "max_active_policies", fmt.Errorf("active policies %d exceeds guardrails.max_active_policies %d", active, *guard.MaxActivePolicies)
		}
	}

	if guard.MaxPoliciesPerMinute != nil {
		scope := rateScope(spec)
		if c.rate != nil {
			reservation, err := c.rate.Reserve(ctx, scope, time.Minute, *guard.MaxPoliciesPerMinute, time.Now().UTC())
			if err != nil {
				if errors.Is(err, ratelimit.ErrRateLimitExceeded) {
					releaseAll()
					return nil, "rate_limit_exceeded", fmt.Errorf("rate limit exceeded for scope %s", scope)
				}
				releaseAll()
				return nil, "rate_limit_error", err
			}
			reservations = append(reservations, reservation)
		} else {
			now := time.Now().UTC()
			var count int
			if scope == globalScopeKey {
				count = c.store.RecentCount(time.Minute, now)
			} else {
				count = c.store.RecentCountFor(scope, time.Minute, now)
			}
			if int64(count) >= *guard.MaxPoliciesPerMinute {
				return nil, "rate_limit_exceeded", fmt.Errorf("policies applied in last minute %d exceeds guardrails.max_policies_per_minute %d", count, *guard.MaxPoliciesPerMinute)
			}
		}
	}

	if guard.RateLimit != nil {
		window := time.Duration(guard.RateLimit.WindowSeconds) * time.Second
		if window <= 0 {
			window = time.Minute
		}
		scope := windowScope(spec)
		if c.rate != nil {
			reservation, err := c.rate.Reserve(ctx, scope, window, guard.RateLimit.MaxActions, time.Now().UTC())
			if err != nil {
				if errors.Is(err, ratelimit.ErrRateLimitExceeded) {
					releaseAll()
					return nil, "rate_limit_exceeded", fmt.Errorf("rate limit exceeded for scope %s", scope)
				}
				releaseAll()
				return nil, "rate_limit_error", err
			}
			reservations = append(reservations, reservation)
		} else {
			now := time.Now().UTC()
			count := c.store.RecentCount(window, now)
			if int64(count) >= guard.RateLimit.MaxActions {
				return nil, "rate_limit_exceeded", fmt.Errorf("policies applied in last %v %d exceeds guardrails.rate_limit.max_actions %d", window, count, guard.RateLimit.MaxActions)
			}
		}
	}

	if guard.RateLimitPerTenant != nil {
		tenant := effectiveTenant(spec)
		if tenant == "" {
			releaseAll()
			return nil, "tenant_missing", fmt.Errorf("rate_limit_per_tenant configured but tenant metadata missing")
		}
		scope := tenantScope(tenant)
		if c.rate != nil {
			reservation, err := c.rate.Reserve(ctx, scope, time.Minute, *guard.RateLimitPerTenant, time.Now().UTC())
			if err != nil {
				if errors.Is(err, ratelimit.ErrRateLimitExceeded) {
					releaseAll()
					return nil, "tenant_rate_limit", fmt.Errorf("tenant %s exceeded rate limit", tenant)
				}
				releaseAll()
				return nil, "rate_limit_error", err
			}
			reservations = append(reservations, reservation)
		} else {
			now := time.Now().UTC()
			count := c.store.RecentCountFor(scope, time.Minute, now)
			if int64(count) >= *guard.RateLimitPerTenant {
				return nil, "tenant_rate_limit", fmt.Errorf("tenant %s exceeded rate limit", tenant)
			}
		}
	}

	if guard.RateLimitPerRegion != nil {
		region := effectiveRegion(spec)
		if region == "" {
			releaseAll()
			return nil, "region_missing", fmt.Errorf("rate_limit_per_region configured but region metadata missing")
		}
		scope := regionScope(region)
		if c.rate != nil {
			reservation, err := c.rate.Reserve(ctx, scope, time.Minute, *guard.RateLimitPerRegion, time.Now().UTC())
			if err != nil {
				if errors.Is(err, ratelimit.ErrRateLimitExceeded) {
					releaseAll()
					return nil, "region_rate_limit", fmt.Errorf("region %s exceeded rate limit", region)
				}
				releaseAll()
				return nil, "rate_limit_error", err
			}
			reservations = append(reservations, reservation)
		} else {
			now := time.Now().UTC()
			count := c.store.RecentCountFor(scope, time.Minute, now)
			if int64(count) >= *guard.RateLimitPerRegion {
				return nil, "region_rate_limit", fmt.Errorf("region %s exceeded rate limit", region)
			}
		}
	}

	if len(reservations) == 0 {
		return nil, "", nil
	}
	return reservationSet{reservations: reservations}, "", nil
}

// EvaluateGuardrails exposes guardrail evaluation for testing and diagnostics.
// It returns the guardrail violation reason (if any) without committing reservations.
func (c *Controller) EvaluateGuardrails(ctx context.Context, spec policy.PolicySpec) (string, error) {
	reservation, reason, err := c.checkGuardrails(ctx, spec)
	if reservation != nil {
		_ = reservation.Release(ctx)
	}
	return reason, err
}

func rateScope(spec policy.PolicySpec) string {
	return policy.RateScope(spec)
}

func windowScope(spec policy.PolicySpec) string {
	return policy.WindowScope(spec)
}

func approvalKey(spec policy.PolicySpec) string {
	return fmt.Sprintf("%s:%s", spec.ID, scopeIdentifier(spec))
}

func isApprovalAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve", "approved", "ack", "acknowledge", "approval":
		return true
	default:
		return false
	}
}

func isRejectAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "reject", "rejected", "deny", "denied":
		return true
	default:
		return false
	}
}

func mergePendingDecisionContext(pending policy.Event, decision policy.Event) policy.Event {
	merged := pending
	if merged.Spec.Raw == nil {
		merged.Spec.Raw = map[string]any{}
	}
	if pending.Proto != nil {
		clone := proto.Clone(pending.Proto)
		if evt, ok := clone.(*pb.PolicyUpdateEvent); ok {
			if decision.Proto != nil {
				evt.Action = decision.Proto.GetAction()
				evt.RequiresAck = decision.Proto.GetRequiresAck()
			}
			merged.Proto = evt
		}
	}
	copyDecisionField := func(key string) {
		if val, ok := decision.Spec.Raw[key].(string); ok && strings.TrimSpace(val) != "" {
			merged.Spec.Raw[key] = strings.TrimSpace(val)
		}
	}
	for _, key := range []string{"request_id", "command_id", "workflow_id", "control_action"} {
		copyDecisionField(key)
	}
	decisionMetadata, _ := decision.Spec.Raw["metadata"].(map[string]any)
	mergedMetadata, _ := merged.Spec.Raw["metadata"].(map[string]any)
	if mergedMetadata == nil {
		mergedMetadata = map[string]any{}
		merged.Spec.Raw["metadata"] = mergedMetadata
	}
	for _, key := range []string{"request_id", "command_id", "workflow_id", "decision_reason_code", "decision_reason_text", "control_action"} {
		if val, ok := decisionMetadata[key].(string); ok && strings.TrimSpace(val) != "" {
			mergedMetadata[key] = strings.TrimSpace(val)
		}
	}
	mergedTrace, _ := merged.Spec.Raw["trace"].(map[string]any)
	if mergedTrace == nil {
		mergedTrace = map[string]any{}
		merged.Spec.Raw["trace"] = mergedTrace
	}
	if decisionTrace, ok := decision.Spec.Raw["trace"].(map[string]any); ok {
		for _, key := range []string{"request_id", "command_id", "workflow_id"} {
			if val, ok := decisionTrace[key].(string); ok && strings.TrimSpace(val) != "" {
				mergedTrace[key] = strings.TrimSpace(val)
			}
		}
	}
	return merged
}

func rejectDecisionReason(spec policy.PolicySpec) string {
	if metadata, ok := spec.Raw["metadata"].(map[string]any); ok {
		if val, ok := metadata["decision_reason_text"].(string); ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
		if val, ok := metadata["decision_reason_code"].(string); ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
	}
	return "policy manually rejected"
}

func isRemoveAction(evt policy.Event) bool {
	if strings.EqualFold(strings.TrimSpace(evt.Spec.Action), "remove") {
		return true
	}
	if evt.Proto != nil && strings.EqualFold(strings.TrimSpace(evt.Proto.GetAction()), "remove") {
		return true
	}
	return false
}

func isFastEvent(evt policy.Event) bool {
	return evt.Fast != nil
}

func evtRuleHash(evt policy.Event) []byte {
	if evt.Proto != nil {
		return append([]byte(nil), evt.Proto.GetRuleHash()...)
	}
	if evt.Fast != nil {
		raw, err := hex.DecodeString(strings.TrimSpace(evt.Fast.ContentHash))
		if err == nil {
			return raw
		}
	}
	return nil
}

func evtProducerID(evt policy.Event) []byte {
	if evt.Proto != nil {
		return append([]byte(nil), evt.Proto.GetProducerId()...)
	}
	if evt.Fast != nil {
		raw, err := hex.DecodeString(strings.TrimSpace(evt.Fast.Pubkey))
		if err == nil {
			return raw
		}
	}
	return nil
}

func (c *Controller) HandleFastEvent(ctx context.Context, evt policy.Event) error {
	consumeStart := time.Now()
	if c.metrics != nil {
		c.metrics.ObserveIngest()
		c.metrics.ObserveValidated()
	}
	if evt.Fast == nil {
		if c.metrics != nil {
			c.metrics.ObserveRejected()
		}
		return fmt.Errorf("controller: fast event missing envelope")
	}
	requiresAck := true
	appliedAt := time.Time{}
	fastPath := FastPathEligibility{Eligible: true, Reason: "fast_mitigation"}

	if reason, replayErr := c.checkFastReplay(evt, time.Now().UTC()); replayErr != nil {
		if c.metrics != nil {
			c.metrics.ObserveGuardrailViolation(reason)
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, reason, replayErr.Error(), appliedAt)
		return nil
	}

	if c.killSwitch != nil && c.killSwitch.Enabled() {
		if c.metrics != nil {
			c.metrics.ObserveGuardrailViolation("kill_switch")
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "kill_switch", "enforcement disabled via kill switch", appliedAt)
		return nil
	}

	if c.isDuplicate(evt) {
		appliedAt = time.Now().UTC()
		if c.metrics != nil {
			c.metrics.ObserveConsumeToApply("duplicate", time.Since(consumeStart))
			c.metrics.ObserveDuplicateScope(scopeKindLabel(evt.Spec))
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "duplicate_noop", "fast mitigation duplicate detected", appliedAt)
		return nil
	}

	refreshReason := "applied_provisional"
	refreshOnly := false
	if c.store != nil {
		if existing, ok := c.store.Get(evt.Spec.ID); ok {
			if existing.PendingConsensus && existing.Provisional && existing.RenewalCount >= maxFastMitigationRenewals {
				if c.metrics != nil {
					c.metrics.ObserveGuardrailViolation("fast_renewal_exceeded")
				}
				c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "fast_renewal_exceeded", "fast mitigation renewal limit exceeded", appliedAt)
				return nil
			}
			if sameEffectiveRule(existing.Spec, evt.Spec) {
				refreshOnly = true
				refreshReason = "applied_provisional_refresh"
			} else {
				refreshReason = "applied_provisional_reapply"
			}
		}
	}

	if refreshOnly {
		appliedAt = time.Now().UTC()
		persistStart := time.Now()
		if err := c.store.Upsert(evt.Spec, appliedAt); err != nil {
			if c.metrics != nil {
				c.metrics.ObserveApplyPersist("error", time.Since(persistStart))
				c.metrics.ObserveConsumeToApply("error", time.Since(consumeStart))
			}
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "refresh_persist_error", err.Error(), appliedAt)
			return fmt.Errorf("controller: refresh fast mitigation %s: %w", evt.Spec.ID, err)
		}
		if err := c.store.MarkProvisional(evt.Spec.ID, evt.Fast.MitigationID, fastDeadline(evt.Spec, appliedAt), appliedAt); err != nil && c.logger != nil {
			c.logger.Warn("failed to mark fast mitigation pending consensus", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
		}
		if c.metrics != nil {
			c.metrics.ObserveApplyPersist("success", time.Since(persistStart))
			c.metrics.ObserveConsumeToApply("success", time.Since(consumeStart))
			c.metrics.SetActivePolicies(c.store.ActiveCount())
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "", refreshReason, appliedAt)
		return nil
	}

	start := time.Now()
	if err := c.enforcer.Apply(ctx, evt.Spec); err != nil {
		if c.metrics != nil {
			c.metrics.ObserveApplyError(time.Since(start))
			c.metrics.ObserveConsumeToApply("error", time.Since(consumeStart))
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "apply_error", err.Error(), time.Now().UTC())
		return fmt.Errorf("controller: apply fast mitigation %s: %w", evt.Spec.ID, err)
	}
	appliedAt = time.Now().UTC()
	if c.metrics != nil {
		c.metrics.ObserveApplied(time.Since(start))
		c.metrics.ObserveConsumeToApply("success", time.Since(consumeStart))
	}
	c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "", refreshReason, appliedAt)

	persistStart := time.Now()
	if err := c.store.Upsert(evt.Spec, appliedAt); err != nil {
		if c.metrics != nil {
			c.metrics.ObserveApplyPersist("error", time.Since(persistStart))
		}
		return fmt.Errorf("controller: persist fast mitigation %s: %w", evt.Spec.ID, err)
	}
	if err := c.store.MarkProvisional(evt.Spec.ID, evt.Fast.MitigationID, fastDeadline(evt.Spec, appliedAt), appliedAt); err != nil && c.logger != nil {
		c.logger.Warn("failed to mark fast mitigation pending consensus", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
	}
	if c.metrics != nil {
		c.metrics.ObserveApplyPersist("success", time.Since(persistStart))
		c.metrics.SetActivePolicies(c.store.ActiveCount())
	}
	return nil
}

func fastDeadline(spec policy.PolicySpec, appliedAt time.Time) time.Time {
	if spec.Guardrails.FastPathTTLSeconds != nil && *spec.Guardrails.FastPathTTLSeconds > 0 {
		return appliedAt.Add(time.Duration(*spec.Guardrails.FastPathTTLSeconds) * time.Second)
	}
	if spec.Guardrails.TTLSeconds > 0 {
		return appliedAt.Add(time.Duration(spec.Guardrails.TTLSeconds) * time.Second)
	}
	return time.Time{}
}

func (c *Controller) checkFastReplay(evt policy.Event, now time.Time) (string, error) {
	if !c.replay.enabled {
		return "", nil
	}
	if evt.Fast == nil {
		return "replay_missing_envelope", fmt.Errorf("missing fast mitigation envelope")
	}
	eventTS := time.Unix(evt.Fast.Timestamp, 0).UTC()
	if evt.Fast.Timestamp <= 0 {
		return "replay_invalid_timestamp", fmt.Errorf("invalid timestamp in fast mitigation envelope")
	}
	if now.Add(c.replay.futureSkew).Before(eventTS) {
		return "replay_future_timestamp", fmt.Errorf("fast mitigation timestamp %s exceeds future skew %s", eventTS.Format(time.RFC3339), c.replay.futureSkew.String())
	}
	if eventTS.Before(now.Add(-c.replay.window)) {
		return "replay_stale_timestamp", fmt.Errorf("fast mitigation timestamp %s outside replay window %s", eventTS.Format(time.RFC3339), c.replay.window.String())
	}
	key := strings.Join([]string{
		"fast",
		evt.Fast.MitigationID,
		evt.Fast.PolicyID,
		strings.TrimSpace(evt.Fast.ContentHash),
		strings.TrimSpace(evt.Fast.Pubkey),
		strings.TrimSpace(evt.Fast.Nonce),
		strconv.FormatInt(evt.Fast.Timestamp, 10),
	}, "|")
	c.replayMu.Lock()
	defer c.replayMu.Unlock()
	c.pruneReplayLocked(now)
	if seenAt, ok := c.replaySeen[key]; ok {
		if now.Sub(seenAt) <= c.replay.window {
			return "replay_duplicate", fmt.Errorf("duplicate fast mitigation replay key within window")
		}
	}
	c.replaySeen[key] = now
	if len(c.replaySeen) > c.replay.cacheMax {
		c.evictReplayLocked(len(c.replaySeen) - c.replay.cacheMax)
	}
	return "", nil
}

func (c *Controller) savePendingApproval(key string, evt policy.Event) error {
	if c.store == nil || evt.Proto == nil {
		return nil
	}
	payload, err := proto.Marshal(evt.Proto)
	if err != nil {
		return err
	}
	return c.store.SavePendingApproval(key, payload)
}

func (c *Controller) clearPendingApproval(key string) {
	c.pendingMu.Lock()
	delete(c.pending, key)
	c.pendingMu.Unlock()
	if c.store == nil {
		return
	}
	if err := c.store.RemovePendingApproval(key); err != nil && c.logger != nil {
		c.logger.Warn("failed to clear pending approval", zap.String("key", key), zap.Error(err))
	}
}

func (c *Controller) emitAck(ctx context.Context, evt policy.Event, requiresAck bool, fastPath FastPathEligibility, result ack.Result, errorCode, reason string, appliedAt time.Time) {
	ackAttempted := requiresAck && c.acks != nil
	ackStatus := "skipped"
	ackEnqueueAt := time.Time{}
	ackJournaledAt := time.Time{}
	ackedAt := time.Time{}
	if ackAttempted {
		ackStatus = "enqueue_requested"
	}
	defer func() {
		c.recordDecision(DecisionRecord{
			PolicyID:       evt.Spec.ID,
			Action:         evt.Spec.Action,
			Scope:          scopeIdentifier(evt.Spec),
			Tenant:         effectiveTenant(evt.Spec),
			Result:         string(result),
			ErrorCode:      errorCode,
			Reason:         reason,
			AppliedAt:      appliedAt,
			AckEnqueueAt:   ackEnqueueAt,
			AckJournaledAt: ackJournaledAt,
			AckedAt:        ackedAt,
			AckRequired:    requiresAck,
			AckAttempted:   ackAttempted,
			AckStatus:      ackStatus,
		})
	}()

	if !ackAttempted {
		if c.metrics != nil && !appliedAt.IsZero() {
			c.metrics.ObserveApplyToAckEnqueue("skipped", time.Since(appliedAt))
		}
		return
	}
	ackEnqueueAt = time.Now().UTC()
	c.emitStageMarker(evt, stageAckPublishStart, ackEnqueueAt.UnixMilli())
	if c.metrics != nil {
		c.metrics.ObserveGatewayAckPublish("enqueue_requested")
	}
	if c.logger != nil {
		c.logger.Debug(
			"ack publish requested",
			zap.String("policy_id", evt.Spec.ID),
			zap.String("result", string(result)),
			zap.String("error_code", errorCode),
			zap.Bool("fast_path", fastPath.Eligible),
		)
	}
	payload := ack.Payload{
		Event:      evt,
		Result:     result,
		Reason:     reason,
		ErrorCode:  errorCode,
		AppliedAt:  appliedAt,
		FastPath:   fastPath.Eligible,
		Scope:      scopeIdentifier(evt.Spec),
		Tenant:     effectiveTenant(evt.Spec),
		Region:     effectiveRegion(evt.Spec),
		Controller: c.instanceID,
		QCRef:      extractQCReference(evt.Spec),
		RuleHash:   evtRuleHash(evt),
		ProducerID: evtProducerID(evt),
	}
	ackCtx, cancelAck := context.WithTimeout(context.Background(), c.ackEnqueueTimeout)
	defer cancelAck()
	if err := c.acks.Publish(ackCtx, payload); err != nil {
		if c.metrics != nil && !appliedAt.IsZero() {
			c.metrics.ObserveApplyToAckEnqueue("error", time.Since(appliedAt))
		}
		if c.metrics != nil {
			c.metrics.ObserveGatewayAckPublish("enqueue_failed")
		}
		ackStatus = "enqueue_failed"
		if c.logger != nil {
			c.logger.Error("failed to enqueue ack", zap.String("policy_id", evt.Spec.ID), zap.String("result", string(result)), zap.String("error_code", errorCode), zap.Error(err))
		}
		return
	}
	ackJournaledAt = time.Now().UTC()
	ackedAt = ackJournaledAt
	if c.metrics != nil {
		if !appliedAt.IsZero() {
			c.metrics.ObserveApplyToAckEnqueue("success", time.Since(appliedAt))
		}
		c.metrics.ObserveGatewayAckPublish("enqueue_ok")
	}
	ackStatus = "enqueue_ok"
	c.emitStageMarker(evt, stageAckPublishDone, ackJournaledAt.UnixMilli())
	if c.logger != nil {
		c.logger.Debug("ack publish succeeded", zap.String("policy_id", evt.Spec.ID), zap.String("result", string(result)))
	}
}

func (c *Controller) emitStageMarker(evt policy.Event, stage string, tMs int64, extras ...zap.Field) {
	if c == nil || c.logger == nil || strings.TrimSpace(stage) == "" {
		return
	}
	fields := []zap.Field{
		zap.String("stage", stage),
		zap.String("policy_id", strings.TrimSpace(evt.Spec.ID)),
		zap.Int64("t_ms", tMs),
	}
	if traceID := traceIDFromSpec(evt.Spec); traceID != "" {
		fields = append(fields, zap.String("trace_id", traceID))
	}
	fields = append(fields, extras...)
	c.logger.Info("policy stage marker", fields...)
}

func traceIDFromSpec(spec policy.PolicySpec) string {
	raw := spec.Raw
	if raw == nil {
		return ""
	}
	if traceID := strings.TrimSpace(stringValue(raw["trace_id"])); traceID != "" {
		return traceID
	}
	if md, ok := raw["metadata"].(map[string]any); ok {
		if traceID := strings.TrimSpace(stringValue(md["trace_id"])); traceID != "" {
			return traceID
		}
	}
	if trace, ok := raw["trace"].(map[string]any); ok {
		if traceID := strings.TrimSpace(stringValue(trace["id"])); traceID != "" {
			return traceID
		}
	}
	return ""
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func scopeKindLabel(spec policy.PolicySpec) string {
	scope := strings.ToLower(strings.TrimSpace(spec.Target.Scope))
	switch scope {
	case "namespace", "node", "tenant", "region", "cluster", "global":
		return scope
	case "", "any", "both":
		return "global"
	default:
		return "unknown"
	}
}

func (c *Controller) recordDecision(dec DecisionRecord) {
	if c == nil {
		return
	}
	c.decisionMu.Lock()
	defer c.decisionMu.Unlock()
	c.decisions = append(c.decisions, dec)
	max := c.decMax
	if max <= 0 {
		max = 500
	}
	if len(c.decisions) > max {
		c.decisions = append([]DecisionRecord(nil), c.decisions[len(c.decisions)-max:]...)
	}
}

func (c *Controller) ListDecisions(limit int, policyID string, result string) []DecisionRecord {
	c.decisionMu.RLock()
	defer c.decisionMu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	out := make([]DecisionRecord, 0, limit)
	policyID = strings.TrimSpace(policyID)
	result = strings.ToLower(strings.TrimSpace(result))
	for idx := len(c.decisions) - 1; idx >= 0 && len(out) < limit; idx-- {
		dec := c.decisions[idx]
		if policyID != "" && dec.PolicyID != policyID {
			continue
		}
		if result != "" && strings.ToLower(dec.Result) != result {
			continue
		}
		out = append(out, dec)
	}
	return out
}

func extractQCReference(spec policy.PolicySpec) string {
	if spec.Audit.ReasonCode != "" {
		return spec.Audit.ReasonCode
	}
	if val, ok := spec.Raw["qc_reference"].(string); ok {
		return strings.TrimSpace(val)
	}
	return ""
}

func effectiveTenant(spec policy.PolicySpec) string {
	if spec.Target.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Tenant))
	}
	if spec.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Tenant))
	}
	return ""
}

func effectiveRegion(spec policy.PolicySpec) string {
	if spec.Target.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Region))
	}
	if spec.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Region))
	}
	return ""
}

func effectiveNode(spec policy.PolicySpec) string {
	if node, ok := spec.Target.Selectors["node"]; ok {
		return strings.ToLower(strings.TrimSpace(node))
	}
	return ""
}

func tenantScope(tenant string) string {
	return "tenant:" + tenant
}

func regionScope(region string) string {
	return "region:" + region
}

func scopeIdentifier(spec policy.PolicySpec) string {
	return policy.ScopeIdentifier(spec)
}

func checkAllowlist(spec policy.PolicySpec) (string, error) {
	guard := spec.Guardrails
	if len(guard.AllowlistIPs) == 0 && len(guard.AllowlistCIDRs) == 0 && len(guard.AllowlistNamespaces) == 0 {
		return "", nil
	}

	// direct ip matches (normalized)
	allowIPs := parseAddrs(guard.AllowlistIPs)
	for _, ipStr := range spec.Target.IPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ipStr))
		if err != nil {
			continue
		}
		if containsAddr(allowIPs, addr) {
			return "allowlist_ip", fmt.Errorf("target ip %s is allowlisted", addr.String())
		}
	}

	// namespace allowlist
	ns := effectiveNamespace(spec)
	if ns != "" {
		for _, allowed := range guard.AllowlistNamespaces {
			if strings.EqualFold(ns, normalizeNamespace(allowed)) {
				return "allowlist_namespace", fmt.Errorf("target namespace %s is allowlisted", ns)
			}
		}
	} else if spec.Target.Scope == "namespace" && len(guard.AllowlistNamespaces) > 0 {
		return "allowlist_namespace", fmt.Errorf("target namespace unspecified but namespace allowlist present")
	}

	// CIDR protections
	allowCIDRs := parsePrefixes(guard.AllowlistCIDRs)
	for _, ipStr := range spec.Target.IPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ipStr))
		if err != nil {
			continue
		}
		if prefixContainsAddr(allowCIDRs, addr) {
			return "allowlist_cidr", fmt.Errorf("target ip %s falls within allowlisted cidr", addr.String())
		}
	}

	if len(spec.Target.CIDRs) > 0 {
		targetCIDRs := parsePrefixes(spec.Target.CIDRs)
		for _, allowed := range allowCIDRs {
			for _, target := range targetCIDRs {
				if prefixesOverlap(allowed, target) {
					return "allowlist_cidr", fmt.Errorf("target cidr %s overlaps allowlisted cidr %s", target.String(), allowed.String())
				}
			}
		}
	}

	for _, targetCIDR := range spec.Target.CIDRs {
		if containsPrefix(allowCIDRs, targetCIDR) {
			return "allowlist_cidr", fmt.Errorf("target cidr %s is allowlisted", targetCIDR)
		}
	}

	return "", nil
}

func effectiveNamespace(spec policy.PolicySpec) string {
	if spec.Target.Namespace != "" {
		return spec.Target.Namespace
	}
	if ns, ok := spec.Target.Selectors["namespace"]; ok {
		return strings.ToLower(strings.TrimSpace(ns))
	}
	if spec.Target.Scope == "namespace" {
		return ""
	}
	return ""
}

func normalizeNamespace(ns string) string {
	return strings.ToLower(strings.TrimSpace(ns))
}

func parseAddrs(values []string) []netip.Addr {
	result := make([]netip.Addr, 0, len(values))
	for _, val := range values {
		addr, err := netip.ParseAddr(strings.TrimSpace(val))
		if err == nil {
			result = append(result, addr)
		}
	}
	return result
}

func containsAddr(haystack []netip.Addr, needle netip.Addr) bool {
	for _, addr := range haystack {
		if addr == needle {
			return true
		}
	}
	return false
}

func parsePrefixes(values []string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, val := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(val))
		if err == nil {
			result = append(result, prefix)
		}
	}
	return result
}

func prefixContainsAddr(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func prefixesOverlap(a, b netip.Prefix) bool {
	return a.Overlaps(b)
}

func containsPrefix(prefixes []netip.Prefix, raw string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	for _, p := range prefixes {
		if p == prefix {
			return true
		}
	}
	return false
}

type reservationSet struct {
	reservations []ratelimit.Reservation
}

func (r reservationSet) Commit(ctx context.Context) error {
	var firstErr error
	for _, res := range r.reservations {
		if res == nil {
			continue
		}
		if err := res.Commit(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r reservationSet) Release(ctx context.Context) error {
	var firstErr error
	for _, res := range r.reservations {
		if res == nil {
			continue
		}
		if err := res.Release(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r reservationSet) Count() int64 {
	var total int64
	for _, res := range r.reservations {
		if res == nil {
			continue
		}
		total += res.Count()
	}
	return total
}
