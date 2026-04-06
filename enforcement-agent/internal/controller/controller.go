package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/ack"
	"github.com/CyberMesh/enforcement-agent/internal/consumercontract"
	"github.com/CyberMesh/enforcement-agent/internal/control"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/observability"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
	"github.com/CyberMesh/enforcement-agent/internal/ratelimit"
	"github.com/CyberMesh/enforcement-agent/internal/state"

	pb "backend/proto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Controller bridges Kafka messages to enforcement backend.
type Controller struct {
	trust                         *policy.TrustedKeys
	store                         *state.Store
	enforcer                      enforcer.Enforcer
	metrics                       *metrics.Recorder
	logger                        *zap.Logger
	rate                          ratelimit.Coordinator
	killSwitch                    *control.KillSwitch
	seenHashes                    sync.Map // policyID -> string(hash)
	pending                       map[string]policy.Event
	pendingMu                     sync.Mutex
	fastPath                      FastPathConfig
	acks                          ack.Publisher
	ackEnqueueTimeout             time.Duration
	applyTimeout                  time.Duration
	applyTotalBudget              time.Duration
	applyRetryMax                 int
	applyRetryBackoff             time.Duration
	applyRetryBase                time.Duration
	applyRetryMaxCap              time.Duration
	applyRetryJitter              float64
	applyTimeoutBreakerThreshold  int
	applyTimeoutBreakerCooldown   time.Duration
	applyTimeoutStreak            int64
	applyTimeoutBreakerUntilNs    int64
	enableExpiredShed             bool
	rngMu                         sync.Mutex
	rng                           *rand.Rand
	instanceID                    string
	replay                        replayConfig
	replaySeen                    map[string]time.Time
	replayMu                      sync.Mutex
	scopeLocks                    sync.Map // scope key -> *sync.Mutex
	decisionMu                    sync.RWMutex
	decisions                     []DecisionRecord
	decMax                        int
	commandRejects                CommandRejectSink
	commandTopic                  string
	lifecycleCompactor            *lifecycleCompactor
	criticalSlots                 chan struct{}
	maintenanceSlots              chan struct{}
	criticalQueueDepth            int64
	maintQueueDepth               int64
	laneStarveAfter               time.Duration
	emitAcceptedAck               bool
	criticalMode                  string
	maintenanceMode               string
	lifecyclePromotionGateEnabled bool
	lifecyclePromotionMinWindow   time.Duration
	startedAt                     time.Time
	criticalGateWarned            uint32
	maintenanceGateWarned         uint32
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
	FastPathEnabled               bool
	FastPathMinConfidence         float64
	FastPathSignals               int64
	AckPublisher                  ack.Publisher
	AckEnqueueTimeout             time.Duration
	ApplyTimeout                  time.Duration
	ApplyTotalBudget              time.Duration
	ApplyRetryMax                 int
	ApplyRetryBackoff             time.Duration
	ApplyRetryBaseBackoff         time.Duration
	ApplyRetryMaxBackoff          time.Duration
	ApplyRetryJitterRatio         float64
	ApplyTimeoutBreakerThreshold  int
	ApplyTimeoutBreakerCooldown   time.Duration
	ExpiredShedding               bool
	ControllerInstanceID          string
	ReplayWindow                  time.Duration
	ReplayFutureSkew              time.Duration
	ReplayCacheMaxEntries         int
	CommandRejectSink             CommandRejectSink
	CommandTopic                  string
	LifecycleCompactionEnabled    bool
	LifecycleCompactionWindow     time.Duration
	CriticalLaneMaxInFlight       int
	MaintenanceLaneMaxInFlight    int
	LaneStarvationThreshold       time.Duration
	EmitAcceptedAck               bool
	CriticalMode                  string
	MaintenanceMode               string
	LifecyclePromotionGateEnabled bool
	LifecyclePromotionMinWindow   time.Duration
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
	reservationReleaseTimeout = 2 * time.Second
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
		acks:                         opts.AckPublisher,
		ackEnqueueTimeout:            opts.AckEnqueueTimeout,
		applyTimeout:                 opts.ApplyTimeout,
		applyTotalBudget:             opts.ApplyTotalBudget,
		applyRetryMax:                opts.ApplyRetryMax,
		applyRetryBackoff:            opts.ApplyRetryBackoff,
		applyRetryBase:               opts.ApplyRetryBaseBackoff,
		applyRetryMaxCap:             opts.ApplyRetryMaxBackoff,
		applyRetryJitter:             opts.ApplyRetryJitterRatio,
		applyTimeoutBreakerThreshold: opts.ApplyTimeoutBreakerThreshold,
		applyTimeoutBreakerCooldown:  opts.ApplyTimeoutBreakerCooldown,
		enableExpiredShed:            opts.ExpiredShedding,
		rng:                          rand.New(rand.NewSource(time.Now().UnixNano())),
		instanceID:                   opts.ControllerInstanceID,
		replay: replayConfig{
			window:         replayWindow,
			futureSkew:     replayFutureSkew,
			cacheMax:       replayMax,
			enabled:        replayWindow > 0,
			requirePayload: true,
		},
		replaySeen:                    make(map[string]time.Time, replayMax),
		decMax:                        500,
		commandRejects:                opts.CommandRejectSink,
		commandTopic:                  strings.TrimSpace(opts.CommandTopic),
		emitAcceptedAck:               opts.EmitAcceptedAck,
		criticalMode:                  strings.ToLower(strings.TrimSpace(opts.CriticalMode)),
		maintenanceMode:               strings.ToLower(strings.TrimSpace(opts.MaintenanceMode)),
		lifecyclePromotionGateEnabled: opts.LifecyclePromotionGateEnabled,
		lifecyclePromotionMinWindow:   opts.LifecyclePromotionMinWindow,
		startedAt:                     time.Now().UTC(),
	}
	if c.ackEnqueueTimeout <= 0 {
		c.ackEnqueueTimeout = 500 * time.Millisecond
	}
	if c.applyRetryMax < 0 {
		c.applyRetryMax = 0
	}
	if c.applyRetryBackoff <= 0 {
		c.applyRetryBackoff = 100 * time.Millisecond
	}
	if c.applyRetryBase <= 0 {
		c.applyRetryBase = c.applyRetryBackoff
	}
	if c.applyRetryMaxCap <= 0 {
		c.applyRetryMaxCap = time.Second
	}
	if c.applyRetryMaxCap < c.applyRetryBase {
		c.applyRetryMaxCap = c.applyRetryBase
	}
	if c.applyRetryJitter < 0 {
		c.applyRetryJitter = 0
	}
	if c.applyRetryJitter > 1 {
		c.applyRetryJitter = 1
	}
	if c.applyTimeoutBreakerThreshold < 0 {
		c.applyTimeoutBreakerThreshold = 0
	}
	if c.applyTimeoutBreakerCooldown < 0 {
		c.applyTimeoutBreakerCooldown = 0
	}
	if c.applyTotalBudget < 0 {
		c.applyTotalBudget = 0
	}
	if opts.LifecycleCompactionEnabled {
		window := opts.LifecycleCompactionWindow
		if window <= 0 {
			window = 2 * time.Minute
		}
		c.lifecycleCompactor = newLifecycleCompactor(window)
	}
	criticalCap := opts.CriticalLaneMaxInFlight
	if criticalCap <= 0 {
		criticalCap = 32
	}
	maintCap := opts.MaintenanceLaneMaxInFlight
	if maintCap <= 0 {
		maintCap = 8
	}
	c.criticalSlots = make(chan struct{}, criticalCap)
	c.maintenanceSlots = make(chan struct{}, maintCap)
	if c.criticalMode == "" {
		c.criticalMode = "enforce"
	}
	if c.criticalMode != "enforce" && c.criticalMode != "dry_run" {
		c.criticalMode = "enforce"
	}
	if c.maintenanceMode == "" {
		c.maintenanceMode = "enforce"
	}
	if c.maintenanceMode != "enforce" && c.maintenanceMode != "dry_run" {
		c.maintenanceMode = "enforce"
	}
	c.laneStarveAfter = opts.LaneStarvationThreshold
	if c.laneStarveAfter <= 0 {
		c.laneStarveAfter = 2 * time.Second
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
	ctx, span := observability.Tracer("enforcement-agent/controller").Start(
		ctx,
		"enforcement.handle_message",
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", msg.Topic),
			attribute.Int64("messaging.kafka.partition", int64(msg.Partition)),
			attribute.Int64("messaging.kafka.offset", msg.Offset),
		),
	)
	defer span.End()

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
		span.RecordError(err)
		span.SetStatus(codes.Error, "unmarshal policy event")
		c.metrics.ObserveRejected()
		if c.logger != nil {
			c.logger.Warn("failed to unmarshal policy", zap.Error(err), zap.Int("value_bytes", len(msg.Value)))
		}
		c.publishCommandReject(ctx, msg, err)
		return nil
	}

	evt, err := c.trust.VerifyAndParseWithDomain(&event, c.policyDomainForTopic(msg.Topic))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "verify policy event")
		c.metrics.ObserveRejected()
		if c.logger != nil {
			c.logger.Warn("policy verification failed", zap.String("policy_id", event.PolicyId), zap.Error(err))
		}
		c.publishCommandReject(ctx, msg, err)
		return nil
	}
	span.SetAttributes(attribute.String("policy.id", evt.Spec.ID))
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
		if replayReason == "replay_duplicate" {
			appliedAt = time.Now().UTC()
			if c.logger != nil {
				c.logger.Debug("duplicate replay treated as idempotent noop",
					zap.String("policy_id", evt.Spec.ID),
					zap.Error(replayErr))
			}
			c.emitStageMarker(evt, stageEnforcementApplyDone, appliedAt.UnixMilli(), zap.String("result", "duplicate_noop"))
			if c.metrics != nil {
				c.metrics.ObserveConsumeToApply("duplicate", time.Since(consumeStart))
				c.metrics.ObserveDuplicateScope(scopeKindLabel(evt.Spec))
			}
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "duplicate_noop", "policy replay duplicate detected", appliedAt)
			return nil
		}
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
		if replayReason == "replay_invalid_timestamp" || replayReason == "replay_future_timestamp" {
			c.publishCommandReject(ctx, msg, replayErr)
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, replayReason, replayErr.Error(), appliedAt)
		return nil
	}

	if c.enableExpiredShed {
		now := time.Now().UTC()
		if expiryAt, expired, ok := policyExpiryDeadline(evt, now); ok && expired {
			errorCode := "shed_expired_policy"
			reason := fmt.Sprintf("policy expired before enforcement apply (expiry=%s now=%s)", expiryAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
			if c.metrics != nil {
				c.metrics.ObserveApplyTerminalFailure("expired_shed")
				c.metrics.ObservePolicyShedding("expired")
			}
			if rejectErr := c.publishCommandReject(ctx, msg, errors.New(reason)); rejectErr != nil {
				if c.metrics != nil {
					c.metrics.ObserveRejectPublishFail()
				}
				errorCode = "shed_expired_policy_reject_publish_failed"
				reason = reason + "; reject artifact publish failed: " + rejectErr.Error()
			}
			if c.logger != nil {
				fields := []zap.Field{
					zap.String("policy_id", strings.TrimSpace(evt.Spec.ID)),
					zap.String("error_code", errorCode),
					zap.String("reason", reason),
					zap.Time("expiry_at", expiryAt),
					zap.Time("now", now),
				}
				if msg != nil {
					fields = append(fields,
						zap.String("topic", msg.Topic),
						zap.Int32("partition", msg.Partition),
						zap.Int64("offset", msg.Offset),
					)
				}
				c.logger.Warn("policy shed due to policy-expiry semantics", fields...)
			}
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, errorCode, reason, now)
			return nil
		}
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
		if c.shouldCompactEvent(evt, eventTS, hasEventTS, consumeStart, "critical") {
			appliedAt = time.Now().UTC()
			c.emitStageMarker(evt, stageEnforcementApplyDone, appliedAt.UnixMilli(), zap.String("result", "compacted_noop"))
			advanceScopeWatermark()
			c.commitDuplicateFingerprint(evt)
			c.commitReplayKey(replayKey, time.Now().UTC())
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultNoop, "lifecycle_compacted_superseded", "superseded lifecycle intent compacted", appliedAt)
			if approvedFromPending {
				c.clearPendingApproval(pendingKey)
			}
			return nil
		}
		if c.effectiveLaneMode(laneCritical) == "dry_run" {
			now := time.Now().UTC()
			c.emitStageMarker(evt, stageEnforcementApplyDone, now.UnixMilli(), zap.String("result", "critical_dry_run_noop"))
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultNoop, "lifecycle_class_critical_dry_run", "critical lifecycle class in dry_run mode", now)
			return nil
		}
		releaseLane, laneErr := c.enterLane(ctx, laneCritical)
		if laneErr != nil {
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "critical_lane_wait_failed", laneErr.Error(), time.Now().UTC())
			return laneErr
		}
		defer releaseLane()

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
		// Keep critical-lane occupancy focused on dataplane mutation only.
		// ACK + state persistence happen after slot release.
		releaseLane()
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
			c.releaseReservation(reservation, evt.Spec.ID)
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
	reservationFinalized := false
	finalizeReservation := func(commit bool) {
		if reservation == nil || reservationFinalized {
			return
		}
		if commit {
			if err := reservation.Commit(ctx); err != nil && c.logger != nil {
				c.logger.Warn("rate reservation commit failed", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
			}
		} else {
			c.releaseReservation(reservation, evt.Spec.ID)
		}
		reservationFinalized = true
	}
	defer finalizeReservation(false)
	if c.emitAcceptedAck {
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultAccepted, "accepted_pre_apply", "policy accepted after verification and guardrails", time.Now().UTC())
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
		if c.shouldCompactEvent(evt, eventTS, hasEventTS, consumeStart, "maintenance") {
			appliedAt = time.Now().UTC()
			c.emitStageMarker(evt, stageEnforcementApplyDone, appliedAt.UnixMilli(), zap.String("result", "compacted_noop"))
			advanceScopeWatermark()
			c.commitDuplicateFingerprint(evt)
			c.commitReplayKey(replayKey, time.Now().UTC())
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultNoop, "lifecycle_compacted_superseded", "superseded lifecycle intent compacted", appliedAt)
			if approvedFromPending {
				c.clearPendingApproval(pendingKey)
			}
			return nil
		}
		if c.effectiveLaneMode(laneMaintenance) == "dry_run" {
			now := time.Now().UTC()
			c.emitStageMarker(evt, stageEnforcementApplyDone, now.UnixMilli(), zap.String("result", "maintenance_dry_run_noop"))
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultNoop, "lifecycle_class_maintenance_dry_run", "maintenance lifecycle class in dry_run mode", now)
			return nil
		}

		releaseLane, laneErr := c.enterLane(ctx, laneMaintenance)
		if laneErr != nil {
			c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "maintenance_lane_wait_failed", laneErr.Error(), time.Now().UTC())
			return laneErr
		}
		defer releaseLane()

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
		finalizeReservation(true)
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
	if c.shouldCompactEvent(evt, eventTS, hasEventTS, consumeStart, "critical") {
		appliedAt = time.Now().UTC()
		c.emitStageMarker(evt, stageEnforcementApplyDone, appliedAt.UnixMilli(), zap.String("result", "compacted_noop"))
		advanceScopeWatermark()
		c.commitDuplicateFingerprint(evt)
		c.commitReplayKey(replayKey, time.Now().UTC())
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultNoop, "lifecycle_compacted_superseded", "superseded lifecycle intent compacted", appliedAt)
		if approvedFromPending {
			c.clearPendingApproval(pendingKey)
		}
		return nil
	}
	if c.effectiveLaneMode(laneCritical) == "dry_run" {
		now := time.Now().UTC()
		c.emitStageMarker(evt, stageEnforcementApplyDone, now.UnixMilli(), zap.String("result", "critical_dry_run_noop"))
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultNoop, "lifecycle_class_critical_dry_run", "critical lifecycle class in dry_run mode", now)
		return nil
	}
	releaseLane, laneErr := c.enterLane(ctx, laneCritical)
	if laneErr != nil {
		finalizeReservation(false)
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "critical_lane_wait_failed", laneErr.Error(), time.Now().UTC())
		return laneErr
	}
	defer releaseLane()
	if c.logger != nil {
		c.logger.Debug(
			"policy apply started",
			zap.String("policy_id", evt.Spec.ID),
			zap.String("action", evt.Spec.Action),
			zap.String("rule_type", evt.Spec.RuleType),
			zap.Bool("dry_run", evt.Spec.Guardrails.DryRun),
		)
	}
	if err := c.applyWithTerminalHandling(ctx, msg, evt.Spec, consumeStart, requiresAck, fastPath, evt, "apply policy"); err != nil {
		finalizeReservation(false)
		return err
	}
	// Keep critical-lane occupancy focused on dataplane mutation only.
	// ACK + state persistence happen after slot release.
	releaseLane()
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

	finalizeReservation(true)

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

// MessageLane classifies incoming command into critical vs maintenance lane.
// Default is critical for fail-closed behavior.
func (c *Controller) MessageLane(msg *sarama.ConsumerMessage) string {
	if msg == nil || len(msg.Value) == 0 {
		return "critical"
	}
	var evt pb.PolicyUpdateEvent
	if err := proto.Unmarshal(msg.Value, &evt); err != nil {
		return "critical"
	}
	action := strings.ToLower(strings.TrimSpace(evt.GetAction()))
	switch action {
	case "refresh", "renew", "ttl_refresh", "reconcile", "sync", "heartbeat":
		return "maintenance"
	case "remove", "reject", "rejected", "deny", "denied", "freeze", "drop", "quarantine", "block":
		return "critical"
	default:
		return "critical"
	}
}

func (c *Controller) publishCommandReject(ctx context.Context, msg *sarama.ConsumerMessage, reason error) error {
	if c == nil || c.commandRejects == nil || msg == nil || reason == nil {
		return nil
	}
	ctx, span := observability.Tracer("enforcement-agent/controller").Start(
		ctx,
		"enforcement.command_reject.publish",
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", msg.Topic),
			attribute.Int64("messaging.kafka.partition", int64(msg.Partition)),
			attribute.Int64("messaging.kafka.offset", msg.Offset),
		),
	)
	defer span.End()
	// Best-effort reject artifact publication should not be canceled by the
	// caller's consume-loop context. Use a short detached budget.
	publishCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := c.commandRejects.PublishReject(publishCtx, msg, reason); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "command_reject_publish_failed")
		if c.metrics != nil {
			c.metrics.ObserveCommandRejectPublish("error")
		}
		if c.logger != nil {
			c.logger.Warn("command reject artifact publish failed", zap.Error(err))
		}
		return err
	}
	if c.metrics != nil {
		c.metrics.ObserveCommandRejectPublish("success")
	}
	span.SetStatus(codes.Ok, "command_reject_publish_ok")
	return nil
}

func (c *Controller) policyDomainForTopic(topic string) string {
	t := strings.TrimSpace(topic)
	if t == "" {
		return "control.policy.v2"
	}
	if c != nil && strings.TrimSpace(c.commandTopic) != "" {
		ct := strings.TrimSpace(c.commandTopic)
		// Legacy fallback deployments may set commandTopic == control.policy.v2.
		// In that compatibility mode, keep legacy signing domain semantics.
		if ct == "control.policy.v2" {
			return "control.policy.v2"
		}
		if t == ct {
			return "control.enforcement_command.v1"
		}
	}
	if t == "control.enforcement_command.v1" {
		return "control.enforcement_command.v1"
	}
	return "control.policy.v2"
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
	if cmdID := eventCommandID(evt); cmdID != "" {
		return "cmd:" + cmdID
	}
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

func eventCommandID(evt policy.Event) string {
	if evt.Spec.Raw != nil {
		if id := strings.TrimSpace(stringValue(evt.Spec.Raw["command_id"])); id != "" {
			return id
		}
		if md, ok := evt.Spec.Raw["metadata"].(map[string]any); ok {
			if id := strings.TrimSpace(stringValue(md["command_id"])); id != "" {
				return id
			}
		}
		if tr, ok := evt.Spec.Raw["trace"].(map[string]any); ok {
			if id := strings.TrimSpace(stringValue(tr["command_id"])); id != "" {
				return id
			}
		}
	}
	return ""
}

func policyEventTimestamp(evt policy.Event) (time.Time, bool) {
	if evt.Proto == nil || evt.Proto.GetTimestamp() <= 0 {
		return time.Time{}, false
	}
	return time.Unix(evt.Proto.GetTimestamp(), 0).UTC(), true
}

func policyExpiryDeadline(evt policy.Event, now time.Time) (time.Time, bool, bool) {
	if evt.Spec.Guardrails.TTLSeconds <= 0 {
		return time.Time{}, false, false
	}
	ts, ok := policyEventTimestamp(evt)
	if !ok {
		return time.Time{}, false, false
	}
	deadline := ts.Add(time.Duration(evt.Spec.Guardrails.TTLSeconds) * time.Second)
	return deadline, now.After(deadline), true
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
				c.releaseReservation(r, strings.TrimSpace(spec.ID))
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
		c.releaseReservation(reservation, strings.TrimSpace(spec.ID))
	}
	return reason, err
}

func (c *Controller) releaseReservation(res ratelimit.Reservation, policyID string) {
	if res == nil {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), reservationReleaseTimeout)
	defer cancel()
	if err := res.Release(releaseCtx); err != nil && c.logger != nil {
		c.logger.Warn("rate reservation release failed",
			zap.String("policy_id", strings.TrimSpace(policyID)),
			zap.Error(err))
	}
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
	if err := c.applyWithTerminalHandling(ctx, nil, evt.Spec, consumeStart, requiresAck, fastPath, evt, "apply fast mitigation"); err != nil {
		return err
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
	ctx, span := observability.Tracer("enforcement-agent/controller").Start(
		ctx,
		"enforcement.ack.emit",
		trace.WithAttributes(
			attribute.String("policy.id", evt.Spec.ID),
			attribute.String("ack.result", string(result)),
			attribute.Bool("ack.required", requiresAck),
		),
	)
	defer span.End()
	result = canonicalAckResult(result, errorCode, reason)
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
		span.SetStatus(codes.Ok, "ack_skipped")
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
		span.RecordError(err)
		span.SetStatus(codes.Error, "ack_enqueue_failed")
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
	span.SetStatus(codes.Ok, "ack_enqueue_ok")
	if c.logger != nil {
		c.logger.Debug("ack publish succeeded", zap.String("policy_id", evt.Spec.ID), zap.String("result", string(result)))
	}
}

func canonicalAckResult(result ack.Result, errorCode, reason string) ack.Result {
	res := strings.ToLower(strings.TrimSpace(string(result)))
	code := strings.ToLower(strings.TrimSpace(errorCode))
	rsn := strings.ToLower(strings.TrimSpace(reason))

	isTimeout := strings.Contains(code, "timeout") || strings.Contains(code, "deadline") ||
		strings.Contains(rsn, "timeout") || strings.Contains(rsn, "deadline")
	isRetryExhausted := strings.Contains(code, "retry_exhausted") || strings.Contains(code, "retries_exhausted") ||
		strings.Contains(rsn, "retry_exhausted") || strings.Contains(rsn, "retries_exhausted")
	isNoop := strings.Contains(code, "noop") || strings.Contains(code, "duplicate") || strings.Contains(rsn, "noop")

	switch res {
	case string(ack.ResultAccepted):
		return ack.ResultAccepted
	case string(ack.ResultApplied):
		if isNoop {
			return ack.ResultNoop
		}
		return ack.ResultApplied
	case string(ack.ResultNoop):
		return ack.ResultNoop
	case string(ack.ResultRejected):
		return ack.ResultRejected
	case string(ack.ResultTimeout):
		return ack.ResultTimeout
	case string(ack.ResultRetryExhausted):
		return ack.ResultRetryExhausted
	case string(ack.ResultFailed):
		if isTimeout {
			return ack.ResultTimeout
		}
		if isRetryExhausted {
			return ack.ResultRetryExhausted
		}
		return ack.ResultRejected
	default:
		if isTimeout {
			return ack.ResultTimeout
		}
		if isRetryExhausted {
			return ack.ResultRetryExhausted
		}
		if isNoop {
			return ack.ResultNoop
		}
		if res == "" {
			return ack.ResultRejected
		}
		return ack.Result(res)
	}
}

func (c *Controller) applyWithTimeout(ctx context.Context, spec policy.PolicySpec) error {
	applyCtx := ctx
	cancel := func() {}
	if c.applyTimeout > 0 {
		applyCtx, cancel = context.WithTimeout(ctx, c.applyTimeout)
	}
	defer cancel()
	start := time.Now()
	err := c.enforcer.Apply(applyCtx, spec)
	elapsed := time.Since(start)
	if c.applyTimeout > 0 && err != nil {
		if class, ok := applyErrorClass(err); ok && class == consumercontract.ErrorClassTimeout && elapsed >= c.applyTimeout {
			if c.metrics != nil {
				c.metrics.ObserveApplyTimeoutBudgetExceeded()
			}
			if c.logger != nil {
				c.logger.Warn("apply attempt exceeded timeout budget",
					zap.String("policy_id", strings.TrimSpace(spec.ID)),
					zap.Duration("attempt_elapsed", elapsed),
					zap.Duration("attempt_budget", c.applyTimeout),
					zap.Error(err))
			}
		}
	}
	return err
}

func (c *Controller) applyWithRetries(ctx context.Context, spec policy.PolicySpec) (error, int, bool) {
	retryCtx := ctx
	cancelRetryCtx := func() {}
	if c.applyTotalBudget > 0 {
		// Do not inherit caller deadlines (e.g. consumer handler budget) so retry
		// policy remains deterministic; still honor explicit cancellation.
		retryCtx, cancelRetryCtx = context.WithTimeout(context.WithoutCancel(ctx), c.applyTotalBudget)
	}
	defer cancelRetryCtx()

	maxAttempts := c.applyRetryMax + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	backoff := c.applyRetryBase
	if backoff <= 0 {
		backoff = c.applyRetryBackoff
	}
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err(), attempt - 1, false
		}
		err := c.applyWithTimeout(retryCtx, spec)
		if err == nil {
			return nil, attempt, false
		}
		lastErr = err
		class, ok := applyErrorClass(err)
		retryable := ok && (class == consumercontract.ErrorClassTimeout || class == consumercontract.ErrorClassTransient)
		if !retryable {
			return lastErr, attempt, false
		}
		if attempt >= maxAttempts {
			return lastErr, attempt, true
		}
		if c.metrics != nil {
			c.metrics.ObserveApplyRetry(string(class))
		}
		wait := c.jitteredBackoff(backoff)
		if !waitForBackoff(retryCtx, ctx, wait) {
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err(), attempt, false
			}
			if cerr := retryCtx.Err(); cerr != nil {
				return cerr, attempt, false
			}
			return context.Canceled, attempt, false
		}
		if backoff < c.applyRetryMaxCap {
			backoff *= 2
			if backoff > c.applyRetryMaxCap {
				backoff = c.applyRetryMaxCap
			}
		}
	}
	return lastErr, maxAttempts, true
}

func (c *Controller) jitteredBackoff(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	wait := base
	if c.applyRetryJitter > 0 && c.rng != nil {
		span := float64(base) * c.applyRetryJitter
		c.rngMu.Lock()
		j := (c.rng.Float64()*2 - 1) * span
		c.rngMu.Unlock()
		wait = time.Duration(float64(base) + j)
		if wait < 0 {
			wait = 0
		}
	}
	if c.applyRetryMaxCap > 0 && wait > c.applyRetryMaxCap {
		wait = c.applyRetryMaxCap
	}
	return wait
}

func waitForBackoff(retryCtx context.Context, parentCtx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	deadline := time.Now().Add(d)
	parentDone := parentCtx.Done()
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return true
		}
		timer := time.NewTimer(remaining)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return false
		case <-parentDone:
			timer.Stop()
			// Honor explicit cancellation (shutdown/rebalance), but ignore parent
			// deadlines so retry budget remains internally deterministic.
			if errors.Is(parentCtx.Err(), context.Canceled) {
				return false
			}
			// Parent deadline exceeded: stop listening to parent done channel for
			// this backoff window to avoid hot-looping until retryCtx expires.
			parentDone = nil
		case <-timer.C:
			return true
		}
	}
}

func (c *Controller) applyWithTerminalHandling(
	ctx context.Context,
	msg *sarama.ConsumerMessage,
	spec policy.PolicySpec,
	consumeStart time.Time,
	requiresAck bool,
	fastPath FastPathEligibility,
	evt policy.Event,
	op string,
) error {
	if c.isApplyTimeoutBreakerOpen(time.Now()) {
		if c.metrics != nil {
			c.metrics.ObserveApplyTimeoutBreakerSkip()
		}
		reason := "apply timeout breaker open; skipping apply attempt during cooldown"
		if c.logger != nil {
			c.logger.Warn("apply timeout breaker skipping apply",
				zap.String("policy_id", strings.TrimSpace(spec.ID)),
				zap.String("op", op))
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultTimeout, "apply_timeout_breaker_open", reason, time.Now().UTC())
		return fmt.Errorf("controller: %s %s: breaker open", op, spec.ID)
	}

	start := time.Now()
	err, attempts, exhausted := c.applyWithRetries(ctx, spec)
	if err == nil {
		c.recordApplyTimeoutOutcome(false)
		return nil
	}
	if c.metrics != nil {
		c.metrics.ObserveApplyError(time.Since(start))
		c.metrics.ObserveConsumeToApply("error", time.Since(consumeStart))
	}
	errorCode := applyErrorCode(err)
	result := ack.ResultFailed
	reason := err.Error()
	class, classOK := applyErrorClass(err)
	c.recordApplyTimeoutOutcome(classOK && class == consumercontract.ErrorClassTimeout)
	classLabel := "unknown"
	if classOK {
		classLabel = string(class)
		if class == consumercontract.ErrorClassTimeout && c.metrics != nil {
			c.metrics.ObserveTimeout()
		}
	}
	decision := terminalRejectPolicy(class, classOK, exhausted)
	if exhausted {
		result = ack.ResultRetryExhausted
		errorCode = "apply_retry_exhausted"
		reason = fmt.Sprintf("apply retries exhausted after %d attempts: %s", attempts, err.Error())
		if c.metrics != nil {
			c.metrics.ObserveApplyTerminalFailure(decision.metricReason)
			c.metrics.ObserveApplyRetryExhausted()
		}
		if decision.publishReject {
			if rejectErr := c.publishCommandReject(ctx, msg, err); rejectErr != nil {
				if c.metrics != nil {
					c.metrics.ObserveRejectPublishFail()
				}
				errorCode = "apply_retry_exhausted_reject_publish_failed"
				reason = reason + "; reject artifact publish failed: " + rejectErr.Error()
			}
		}
	} else {
		if c.metrics != nil {
			c.metrics.ObserveApplyTerminalFailure(decision.metricReason)
		}
		if decision.publishReject {
			if rejectErr := c.publishCommandReject(ctx, msg, err); rejectErr != nil {
				if c.metrics != nil {
					c.metrics.ObserveRejectPublishFail()
				}
				if classOK && class == consumercontract.ErrorClassPermanent {
					errorCode = "apply_error_reject_publish_failed"
				} else {
					errorCode = "apply_terminal_reject_publish_failed"
				}
				reason = reason + "; reject artifact publish failed: " + rejectErr.Error()
			}
		}
	}
	if c.logger != nil {
		fields := []zap.Field{
			zap.String("policy_id", strings.TrimSpace(spec.ID)),
			zap.String("class", classLabel),
			zap.Int("attempts", attempts),
			zap.Bool("retry_exhausted", exhausted),
			zap.String("error_code", errorCode),
			zap.String("reason", reason),
			zap.Bool("reject_artifact_enabled", decision.publishReject),
			zap.String("reject_policy", decision.policy),
			zap.Error(err),
		}
		if msg != nil {
			fields = append(fields,
				zap.String("topic", msg.Topic),
				zap.Int32("partition", msg.Partition),
				zap.Int64("offset", msg.Offset),
			)
		}
		c.logger.Warn("policy apply terminal failure", fields...)
	}
	c.emitAck(ctx, evt, requiresAck, fastPath, result, errorCode, reason, time.Now().UTC())
	return fmt.Errorf("controller: %s %s: %w", op, spec.ID, err)
}

func (c *Controller) isApplyTimeoutBreakerOpen(now time.Time) bool {
	if c == nil || c.applyTimeoutBreakerCooldown <= 0 || c.applyTimeoutBreakerThreshold <= 0 {
		return false
	}
	untilNs := atomic.LoadInt64(&c.applyTimeoutBreakerUntilNs)
	if untilNs <= 0 {
		return false
	}
	return now.UnixNano() < untilNs
}

func (c *Controller) recordApplyTimeoutOutcome(timeout bool) {
	if c == nil {
		return
	}
	if !timeout {
		atomic.StoreInt64(&c.applyTimeoutStreak, 0)
		return
	}
	if c.applyTimeoutBreakerCooldown <= 0 || c.applyTimeoutBreakerThreshold <= 0 {
		return
	}
	streak := atomic.AddInt64(&c.applyTimeoutStreak, 1)
	if int(streak) < c.applyTimeoutBreakerThreshold {
		return
	}
	openUntil := time.Now().Add(c.applyTimeoutBreakerCooldown).UnixNano()
	atomic.StoreInt64(&c.applyTimeoutBreakerUntilNs, openUntil)
	atomic.StoreInt64(&c.applyTimeoutStreak, 0)
	if c.metrics != nil {
		c.metrics.ObserveApplyTimeoutBreakerOpen()
	}
	if c.logger != nil {
		c.logger.Warn("apply timeout breaker opened",
			zap.Int("threshold", c.applyTimeoutBreakerThreshold),
			zap.Duration("cooldown", c.applyTimeoutBreakerCooldown))
	}
}

type rejectPolicyDecision struct {
	publishReject bool
	metricReason  string
	policy        string
}

// terminalRejectPolicy centralizes terminal-failure reject behavior:
// - retry_exhausted: publish reject artifact (always).
// - permanent: publish reject artifact (always).
// - timeout/transient/canceled: no reject artifact, ACK + metric only.
// - unknown classification: publish reject to fail closed.
func terminalRejectPolicy(class consumercontract.ErrorClass, classOK bool, exhausted bool) rejectPolicyDecision {
	if exhausted {
		return rejectPolicyDecision{
			publishReject: true,
			metricReason:  "retry_exhausted",
			policy:        "retry_exhausted_publish",
		}
	}
	if !classOK {
		return rejectPolicyDecision{
			publishReject: true,
			metricReason:  "unknown",
			policy:        "unknown_fail_closed_publish",
		}
	}
	switch class {
	case consumercontract.ErrorClassPermanent:
		return rejectPolicyDecision{
			publishReject: true,
			metricReason:  "permanent",
			policy:        "permanent_publish",
		}
	case consumercontract.ErrorClassTimeout, consumercontract.ErrorClassTransient:
		return rejectPolicyDecision{
			publishReject: false,
			metricReason:  "retry_skipped",
			policy:        "retryable_no_reject",
		}
	case consumercontract.ErrorClassCanceled:
		return rejectPolicyDecision{
			publishReject: false,
			metricReason:  "canceled",
			policy:        "canceled_no_reject",
		}
	default:
		return rejectPolicyDecision{
			publishReject: true,
			metricReason:  "unknown",
			policy:        "default_fail_closed_publish",
		}
	}
}

func applyErrorCode(err error) string {
	class, ok := applyErrorClass(err)
	if !ok {
		return "apply_error"
	}
	switch class {
	case consumercontract.ErrorClassCanceled:
		return "apply_canceled"
	case consumercontract.ErrorClassTimeout:
		return "apply_timeout"
	default:
		return "apply_error"
	}
}

func applyErrorClass(err error) (consumercontract.ErrorClass, bool) {
	if err == nil {
		return "", false
	}
	if class, ok := consumercontract.ClassifyContextError(err); ok {
		return class, true
	}
	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) {
		return consumercontract.ErrorClassTimeout, true
	}
	if apierrors.IsTooManyRequests(err) || apierrors.IsServiceUnavailable(err) || apierrors.IsInternalError(err) {
		return consumercontract.ErrorClassTransient, true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return consumercontract.ErrorClassTimeout, true
		}
		return consumercontract.ErrorClassTransient, true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "deadline exceeded"),
		strings.Contains(msg, "context deadline exceeded"):
		return consumercontract.ErrorClassTimeout, true
	case strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "temporary"),
		strings.Contains(msg, "try again"),
		strings.Contains(msg, "econnreset"),
		strings.Contains(msg, "econnrefused"),
		strings.Contains(msg, "503"),
		strings.Contains(msg, "429"):
		return consumercontract.ErrorClassTransient, true
	}
	return consumercontract.ErrorClassPermanent, true
}

func (c *Controller) shouldCompactEvent(evt policy.Event, eventTS time.Time, hasEventTS bool, consumeStart time.Time, lane string) bool {
	if c == nil || c.lifecycleCompactor == nil {
		return false
	}
	eventStamp := time.Now().UTC()
	if hasEventTS {
		eventStamp = eventTS
	}
	decision := c.lifecycleCompactor.Observe(evt, eventStamp, time.Now().UTC())
	if decision.Superseded {
		if c.metrics != nil {
			c.metrics.ObserveLifecycleCompaction("superseded")
			c.metrics.ObserveConsumeToApply("duplicate", time.Since(consumeStart))
		}
		if c.logger != nil {
			c.logger.Info("lifecycle intent compacted as superseded",
				zap.String("policy_id", strings.TrimSpace(evt.Spec.ID)),
				zap.String("lane", strings.TrimSpace(lane)),
				zap.String("event_action", decision.Action),
				zap.String("last_action", decision.LastAction),
				zap.Time("last_seen_at", decision.LastSeen))
		}
		return true
	}
	if c.metrics != nil {
		c.metrics.ObserveLifecycleCompaction("accepted")
	}
	return false
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
