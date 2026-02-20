package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/enforcer/cilium"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Enforcer implements the gateway profile and delegates policy application to Cilium CRDs.
type Enforcer struct {
	gatewayNS string
	dryRun    bool
	log       *zap.Logger
	inner     Delegate
	metrics   *metrics.Recorder
	guards    GuardrailsConfig
	mat       guardrailMaterialized
	mu        sync.Mutex
	applied   map[string]applyState
}

// New constructs the gateway backend.
func New(cfg Config) (*Enforcer, error) {
	gatewayNS := strings.ToLower(strings.TrimSpace(cfg.GatewayNamespace))
	if gatewayNS == "" {
		return nil, errors.New("gateway: GATEWAY_NAMESPACE is required")
	}

	guards := cfg.Guardrails.withDefaults()
	mat, err := buildGuardrailMaterialized(guards)
	if err != nil {
		return nil, err
	}

	var inner Delegate = cfg.Delegate
	if inner == nil {
		// We apply policies via the existing Cilium CRD backend.
		innerCfg := cilium.Config{
			KubeConfigPath:  cfg.Cilium.KubeConfigPath,
			Context:         cfg.Cilium.Context,
			Namespace:       cfg.Cilium.Namespace,
			QPS:             cfg.Cilium.QPS,
			Burst:           cfg.Cilium.Burst,
			PolicyMode:      cfg.Cilium.PolicyMode,
			PolicyNamespace: cfg.Cilium.PolicyNamespace,
			LabelPrefix:     cfg.Cilium.LabelPrefix,
			DryRun:          cfg.DryRun,
			Logger:          cfg.Logger,
		}
		c, err := cilium.New(innerCfg)
		if err != nil {
			return nil, fmt.Errorf("gateway: init cilium backend: %w", err)
		}
		inner = c
	}

	return &Enforcer{
		gatewayNS: gatewayNS,
		dryRun:    cfg.DryRun,
		log:       cfg.Logger,
		inner:     inner,
		metrics:   cfg.Metrics,
		guards:    guards,
		mat:       mat,
		applied:   make(map[string]applyState),
	}, nil
}

func (e *Enforcer) Apply(ctx context.Context, spec policy.PolicySpec) error {
	if e == nil || e.inner == nil {
		return errors.New("gateway: enforcer not initialized")
	}

	translated, err := translateGatewaySpec(e.gatewayNS, spec)
	if err != nil {
		if e.metrics != nil {
			e.metrics.ObserveGatewayTranslation("error")
			e.metrics.ObserveGatewayApplyFailure("translation_error", 0)
		}
		if e.log != nil {
			e.log.Warn("gateway decision",
				zap.String("policy_id", spec.ID),
				zap.String("decision", "reject"),
				zap.String("reason_code", "translation_error"),
				zap.Error(err))
		}
		return err
	}
	if e.metrics != nil {
		e.metrics.ObserveGatewayTranslation("ok")
	}
	if e.log != nil {
		ruleID, _ := translated.Raw["gateway_rule_id"].(string)
		ruleHash, _ := translated.Raw["gateway_rule_hash"].(string)
		e.log.Debug("gateway translation complete",
			zap.String("policy_id", translated.ID),
			zap.String("rule_id", ruleID),
			zap.String("rule_hash", ruleHash),
			zap.Int("target_cidrs", len(translated.Target.CIDRs)),
			zap.Int("target_ports", len(translated.Target.Ports)),
			zap.Strings("protocols", translated.Target.Protocols))
	}

	ruleHash, _ := translated.Raw["gateway_rule_hash"].(string)
	targetKey := strings.Join(translated.Target.CIDRs, ",") + "|" + strings.Join(translated.Target.Protocols, ",")
	now := time.Now().UTC()

	e.mu.Lock()
	prev, hadPrev := e.applied[translated.ID]
	if hadPrev && prev.ruleHash == ruleHash {
		e.mu.Unlock()
		if e.metrics != nil {
			e.metrics.ObserveGatewayApplySuccess(0)
		}
		if e.log != nil {
			e.log.Info("gateway decision",
				zap.String("policy_id", translated.ID),
				zap.String("rule_hash", ruleHash),
				zap.String("decision", "noop"),
				zap.String("reason_code", "idempotent_duplicate"))
		}
		return nil
	}
	var prevPtr *applyState
	if hadPrev {
		c := prev
		prevPtr = &c
	}
	activeCount := len(e.applied)
	activeTenantCount := 0
	tenant := effectivePolicyTenant(translated)
	for id, s := range e.applied {
		if hadPrev && id == translated.ID {
			continue
		}
		if tenant != "" && s.tenant == tenant {
			activeTenantCount++
		}
	}
	if err := validateGatewayGuardrails(e.guards, e.mat, translated, activeCount, activeTenantCount, prevPtr, now); err != nil {
		e.mu.Unlock()
		reason := classifyGatewayRejectReason(err)
		if e.metrics != nil {
			e.metrics.ObserveGatewayGuardrailReject(reason)
			if reason == "tenant_mismatch" || reason == "tenant_required" {
				e.metrics.ObserveGatewayTenantReject()
			}
			e.metrics.ObserveGatewayApplyFailure(reason, 0)
		}
		if e.log != nil {
			e.log.Warn("gateway decision",
				zap.String("policy_id", translated.ID),
				zap.String("rule_hash", ruleHash),
				zap.String("decision", "reject"),
				zap.String("reason_code", "guardrail_violation"),
				zap.Error(err))
		}
		return err
	}
	e.mu.Unlock()

	// Delegate to Cilium policy implementation. This is intentionally a narrow v1:
	// egress-only deny policy applied to the gateway namespace/workload segment.
	applyStart := time.Now()
	if err := e.inner.Apply(ctx, translated); err != nil {
		if e.metrics != nil {
			e.metrics.ObserveGatewayApplyFailure("delegate_apply_error", time.Since(applyStart))
		}
		if e.log != nil {
			e.log.Warn("gateway decision",
				zap.String("policy_id", translated.ID),
				zap.String("rule_hash", ruleHash),
				zap.String("decision", "error"),
				zap.String("reason_code", "delegate_apply_error"),
				zap.Error(err))
		}
		return err
	}

	e.mu.Lock()
	e.applied[translated.ID] = applyState{
		ruleHash:  ruleHash,
		targetKey: targetKey,
		appliedAt: now,
		action:    translated.Action,
		tenant:    tenant,
	}
	e.mu.Unlock()
	if e.metrics != nil {
		e.metrics.ObserveGatewayApplySuccess(time.Since(applyStart))
		e.metrics.SetGatewayActiveRules(len(e.applied))
	}
	if e.log != nil {
		e.log.Info("gateway decision",
			zap.String("policy_id", translated.ID),
			zap.String("rule_hash", ruleHash),
			zap.String("decision", "apply"),
			zap.String("reason_code", "ok"))
	}
	return nil
}

func (e *Enforcer) Remove(ctx context.Context, policyID string) error {
	if e == nil || e.inner == nil {
		return errors.New("gateway: enforcer not initialized")
	}
	if err := e.inner.Remove(ctx, policyID); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.applied, policyID)
	active := len(e.applied)
	e.mu.Unlock()
	if e.metrics != nil {
		e.metrics.SetGatewayActiveRules(active)
	}
	return nil
}

func (e *Enforcer) List(ctx context.Context) ([]policy.PolicySpec, error) {
	if e == nil || e.inner == nil {
		return nil, errors.New("gateway: enforcer not initialized")
	}
	return e.inner.List(ctx)
}

func validateGatewayProfile(gatewayNS string, spec policy.PolicySpec) error {
	if strings.TrimSpace(spec.ID) == "" {
		return errors.New("gateway: policy id required")
	}

	action := strings.ToLower(strings.TrimSpace(spec.Action))
	if action != "drop" && action != "reject" {
		return fmt.Errorf("gateway: unsupported action %s", spec.Action)
	}

	// v1: enforce egress-only to reduce blast radius and avoid asymmetric routing confusion.
	dir := strings.ToLower(strings.TrimSpace(spec.Target.Direction))
	if dir != "egress" {
		return fmt.Errorf("gateway: direction must be egress, got %s", spec.Target.Direction)
	}

	// v1: require explicit namespace scoping for the gateway segment.
	scope := strings.ToLower(strings.TrimSpace(spec.Target.Scope))
	if scope != "" && scope != "namespace" {
		return fmt.Errorf("gateway: scope must be namespace (or empty), got %s", spec.Target.Scope)
	}

	ns := strings.ToLower(strings.TrimSpace(spec.Target.Namespace))
	if ns == "" {
		if raw, ok := spec.Target.Selectors["namespace"]; ok {
			ns = strings.ToLower(strings.TrimSpace(raw))
		}
	}
	if ns == "" {
		return errors.New("gateway: target namespace required (target.namespace or selectors.namespace)")
	}
	if ns != gatewayNS {
		return fmt.Errorf("gateway: policy targets namespace %s, expected %s", ns, gatewayNS)
	}

	// Require L3 targets (CIDR/IP); identity-only policies are rejected for gateway v1.
	if len(spec.Target.IPs) == 0 && len(spec.Target.CIDRs) == 0 {
		return fmt.Errorf("gateway: policy %s missing target IPs/CIDRs", spec.ID)
	}

	// If ports are provided, protocols must be transport protocols that support ports.
	if len(spec.Target.Ports) > 0 {
		if len(spec.Target.Protocols) == 0 {
			return errors.New("gateway: ports specified without protocol")
		}
	}

	return nil
}

func effectivePolicyTenant(spec policy.PolicySpec) string {
	tenant := strings.ToLower(strings.TrimSpace(spec.Tenant))
	if tenant != "" {
		return tenant
	}
	return strings.ToLower(strings.TrimSpace(spec.Target.Tenant))
}

func classifyGatewayRejectReason(err error) string {
	if err == nil {
		return "unknown"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "tenant is required"):
		return "tenant_required"
	case strings.Contains(msg, "cross-tenant"):
		return "tenant_mismatch"
	case strings.Contains(msg, "target count"):
		return "max_targets"
	case strings.Contains(msg, "port range count"):
		return "max_ports"
	case strings.Contains(msg, "ttl_seconds"):
		return "max_ttl"
	case strings.Contains(msg, "cooldown"):
		return "cooldown"
	case strings.Contains(msg, "protected namespace"):
		return "protected_namespace"
	case strings.Contains(msg, "overlaps protected cidr"):
		return "protected_cidr"
	case strings.Contains(msg, "broad ipv4 cidr") || strings.Contains(msg, "broad ipv6 cidr"):
		return "broad_cidr"
	default:
		return "guardrail_violation"
	}
}
