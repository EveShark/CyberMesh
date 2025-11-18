package enforcer

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/enforcer/iptables"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/kubernetes"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/nftables"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Enforcer defines the behavior required to apply and remove policies.
type Enforcer interface {
	Apply(ctx context.Context, spec policy.PolicySpec) error
	Remove(ctx context.Context, policyID string) error
	List(ctx context.Context) ([]policy.PolicySpec, error)
}

// HealthChecker can be implemented by backends to report health status.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// ReadyChecker can be implemented by backends to signal readiness state.
type ReadyChecker interface {
	ReadyCheck(ctx context.Context) error
}

// Options describe how to construct an enforcement backend.
type Options struct {
	Backend string
	DryRun  bool
	Logger  *zap.Logger

	IPTables   iptables.Config
	NFTables   nftables.Config
	Kubernetes kubernetes.Config
}

// Factory constructs the selected backend based on name.
func Factory(opts Options) (Enforcer, error) {
	switch opts.Backend {
	case "iptables":
		opts.IPTables.DryRun = opts.DryRun
		opts.IPTables.Logger = opts.Logger
		return iptables.New(opts.IPTables)
	case "nftables":
		opts.NFTables.DryRun = opts.DryRun
		opts.NFTables.Logger = opts.Logger
		return nftables.New(opts.NFTables)
	case "kubernetes", "k8s":
		opts.Kubernetes.DryRun = opts.DryRun
		opts.Kubernetes.Logger = opts.Logger
		return kubernetes.New(opts.Kubernetes)
	case "noop":
		return newNoopEnforcer("noop", opts.DryRun), nil
	default:
		return nil, fmt.Errorf("enforcer: unsupported backend %s", opts.Backend)
	}
}

type noopEnforcer struct {
	backend string
	dryRun  bool
	mu      sync.RWMutex
	set     map[string]policy.PolicySpec
}

func newNoopEnforcer(backend string, dryRun bool) Enforcer {
	return &noopEnforcer{backend: backend, dryRun: dryRun, set: make(map[string]policy.PolicySpec)}
}

func (n *noopEnforcer) Apply(_ context.Context, spec policy.PolicySpec) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.set[spec.ID] = spec
	return nil
}

func (n *noopEnforcer) Remove(_ context.Context, policyID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.set, policyID)
	return nil
}

func (n *noopEnforcer) List(_ context.Context) ([]policy.PolicySpec, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]policy.PolicySpec, 0, len(n.set))
	for _, spec := range n.set {
		result = append(result, spec)
	}
	return result, nil
}

func (n *noopEnforcer) HealthCheck(context.Context) error { return nil }

func (n *noopEnforcer) ReadyCheck(ctx context.Context) error { return n.HealthCheck(ctx) }
