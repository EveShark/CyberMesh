package gateway

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Config controls construction of the gateway enforcement backend.
//
// This backend is a Phase 4 North-South enforcement point (PEP) in the sense that
// it enforces policies on a dedicated "gateway" workload segment. In v1 it is
// implemented using Cilium policy CRDs (same mechanics as East-West) but gated
// by a strict "gateway policy profile".
type Config struct {
	// GatewayNamespace is the namespace treated as the gateway segment.
	// Policies that do not target this namespace are rejected.
	GatewayNamespace string

	// DryRun enables dry-run apply behavior.
	DryRun bool

	// Logger is optional.
	Logger *zap.Logger

	// Cilium holds the nested config used to apply Cilium policy CRDs.
	Cilium CiliumConfig

	// Delegate is an optional test/integration hook that allows constructing the gateway
	// backend without Kubernetes access.
	Delegate Delegate

	// Metrics is optional gateway metrics recorder.
	Metrics *metrics.Recorder

	// Guardrails defines gateway-specific safety limits.
	Guardrails GuardrailsConfig
}

// CiliumConfig is the minimal subset of Cilium backend config we need here.
// Keeping this separate avoids re-exporting the full cilium.Config.
type CiliumConfig struct {
	KubeConfigPath  string
	Context         string
	Namespace       string
	QPS             float32
	Burst           int
	PolicyMode      string
	PolicyNamespace string
	LabelPrefix     string
}

// Delegate is the minimal interface required by the gateway backend.
// When unset, the gateway backend constructs an in-cluster Cilium CRD enforcer.
type Delegate interface {
	Apply(ctx context.Context, spec policy.PolicySpec) error
	Remove(ctx context.Context, policyID string) error
	List(ctx context.Context) ([]policy.PolicySpec, error)
}

// GuardrailsConfig defines gateway-side hard limits and protected resources.
type GuardrailsConfig struct {
	MaxTargetsPerPolicy int64
	MaxPortsPerPolicy   int64
	MaxActivePolicies   int64
	MaxActivePerTenant  int64
	MaxTTLSeconds       int64
	RequireTenant       bool
	EnforceTenantMatch  bool
	ApplyCooldown       time.Duration
	RequireCanary       bool
	DenyBroadCIDRs      bool
	MinIPv4Prefix       int
	MinIPv6Prefix       int

	ProtectedCIDRs      []string
	ProtectedIPs        []string
	ProtectedNamespaces []string
}

func (g GuardrailsConfig) withDefaults() GuardrailsConfig {
	out := g
	if out.MaxTargetsPerPolicy <= 0 {
		out.MaxTargetsPerPolicy = 256
	}
	if out.MaxPortsPerPolicy <= 0 {
		out.MaxPortsPerPolicy = 128
	}
	if out.MaxActivePolicies <= 0 {
		out.MaxActivePolicies = 4096
	}
	if out.MaxTTLSeconds <= 0 {
		out.MaxTTLSeconds = 3600
	}
	if out.MinIPv4Prefix <= 0 || out.MinIPv4Prefix > 32 {
		out.MinIPv4Prefix = 8
	}
	if out.MinIPv6Prefix <= 0 || out.MinIPv6Prefix > 128 {
		out.MinIPv6Prefix = 32
	}
	return out
}
