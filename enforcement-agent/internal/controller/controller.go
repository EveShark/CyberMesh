package controller

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
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
	trust      *policy.TrustedKeys
	store      *state.Store
	enforcer   enforcer.Enforcer
	metrics    *metrics.Recorder
	logger     *zap.Logger
	rate       ratelimit.Coordinator
	killSwitch *control.KillSwitch
	seenHashes sync.Map // policyID -> string(hash)
	pending    map[string]policy.Event
	pendingMu  sync.Mutex
	fastPath   FastPathConfig
	acks       ack.Publisher
	instanceID string
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
	ControllerInstanceID  string
}

const (
	namespaceScopePrefix = "namespace:"
	nodeScopePrefix      = "node:"
	clusterScopeKey      = "cluster"
	globalScopeKey       = "global"
)

// New constructs a controller.
func New(trust *policy.TrustedKeys, store *state.Store, backend enforcer.Enforcer, recorder *metrics.Recorder, rate ratelimit.Coordinator, kill *control.KillSwitch, logger *zap.Logger, opts Options) *Controller {
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
		acks:       opts.AckPublisher,
		instanceID: opts.ControllerInstanceID,
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
	c.metrics.ObserveIngest()

	var event pb.PolicyUpdateEvent
	if err := proto.Unmarshal(msg.Value, &event); err != nil {
		c.metrics.ObserveRejected()
		if c.logger != nil {
			c.logger.Warn("failed to unmarshal policy", zap.Error(err))
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

	if c.isDuplicate(evt) {
		if c.metrics != nil {
			c.metrics.ObserveGuardrailViolation("duplicate_rule")
		}
		if c.logger != nil {
			c.logger.Debug("duplicate policy event ignored", zap.String("policy_id", evt.Spec.ID))
		}
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "duplicate_rule", "policy duplicate detected", appliedAt)
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

	pendingKey := approvalKey(evt.Spec)
	requiresApproval := evt.Spec.Guardrails.ApprovalRequired
	approvedFromPending := false
	if isApprovalAction(evt.Proto.GetAction()) {
		c.pendingMu.Lock()
		pendingEvt, ok := c.pending[pendingKey]
		c.pendingMu.Unlock()
		if ok {
			evt = pendingEvt
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
		if c.logger != nil {
			c.logger.Info("policy pending manual approval", zap.String("policy_id", evt.Spec.ID), zap.String("scope", rateScope(evt.Spec)))
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

	if fastPath.Eligible && evt.Spec.Guardrails.FastPathTTLSeconds != nil {
		deadline := time.Now().Add(time.Duration(*evt.Spec.Guardrails.FastPathTTLSeconds) * time.Second)
		if err := c.store.MarkPendingConsensus(evt.Spec.ID, deadline); err != nil && c.logger != nil {
			c.logger.Warn("failed to mark pending consensus", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
		}
	}

	start := time.Now()
	if err := c.enforcer.Apply(ctx, evt.Spec); err != nil {
		if reservation != nil {
			_ = reservation.Release(ctx)
		}
		c.metrics.ObserveApplyError(time.Since(start))
		c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultFailed, "apply_error", err.Error(), time.Now().UTC())
		return fmt.Errorf("controller: apply policy %s: %w", evt.Spec.ID, err)
	}
	appliedAt = time.Now().UTC()
	c.metrics.ObserveApplied(time.Since(start))
	c.emitAck(ctx, evt, requiresAck, fastPath, ack.ResultApplied, "", "", appliedAt)

	if reservation != nil {
		if err := reservation.Commit(ctx); err != nil && c.logger != nil {
			c.logger.Warn("rate reservation commit failed", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
		}
	}

	if err := c.store.Upsert(evt.Spec, time.Now().UTC()); err != nil {
		if c.logger != nil {
			c.logger.Error("failed to persist policy state", zap.String("policy_id", evt.Spec.ID), zap.Error(err))
		}
	} else if c.metrics != nil {
		c.metrics.SetActivePolicies(c.store.ActiveCount())
	}

	if approvedFromPending {
		c.clearPendingApproval(pendingKey)
	}

	if c.logger != nil {
		c.logger.Info("policy applied",
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

func (c *Controller) isDuplicate(evt policy.Event) bool {
	hash := fmt.Sprintf("%x", evt.Proto.RuleHash)
	key := fmt.Sprintf("%s:%s", evt.Spec.ID, scopeIdentifier(evt.Spec))
	prev, ok := c.seenHashes.Load(key)
	if ok && prev.(string) == hash {
		return true
	}
	c.seenHashes.Store(key, hash)
	return false
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
	return scopeIdentifier(spec)
}

func windowScope(spec policy.PolicySpec) string {
	return "window:" + rateScope(spec)
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
	if !requiresAck || c.acks == nil {
		return
	}
	payload := ack.Payload{
		Event:      evt,
		Result:     result,
		Reason:     reason,
		ErrorCode:  errorCode,
		AppliedAt:  appliedAt,
		AckedAt:    time.Now().UTC(),
		FastPath:   fastPath.Eligible,
		Scope:      scopeIdentifier(evt.Spec),
		Tenant:     effectiveTenant(evt.Spec),
		Region:     effectiveRegion(evt.Spec),
		Controller: c.instanceID,
		QCRef:      extractQCReference(evt.Spec),
		RuleHash:   append([]byte(nil), evt.Proto.GetRuleHash()...),
		ProducerID: append([]byte(nil), evt.Proto.GetProducerId()...),
	}
	if err := c.acks.Publish(ctx, payload); err != nil && c.logger != nil {
		c.logger.Error("failed to enqueue ack", zap.String("policy_id", evt.Spec.ID), zap.String("result", string(result)), zap.String("error_code", errorCode), zap.Error(err))
	}
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
	scope := strings.ToLower(strings.TrimSpace(spec.Target.Scope))
	switch scope {
	case "namespace":
		if ns := effectiveNamespace(spec); ns != "" {
			return namespaceScopePrefix + ns
		}
		return globalScopeKey
	case "tenant":
		if tenant := effectiveTenant(spec); tenant != "" {
			return tenantScope(tenant)
		}
		return globalScopeKey
	case "region":
		if region := effectiveRegion(spec); region != "" {
			return regionScope(region)
		}
		return globalScopeKey
	case "node":
		if node := effectiveNode(spec); node != "" {
			return nodeScopePrefix + node
		}
		return globalScopeKey
	case "cluster":
		return clusterScopeKey
	case "global", "", "any", "both":
		return globalScopeKey
	default:
		return scope
	}
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
