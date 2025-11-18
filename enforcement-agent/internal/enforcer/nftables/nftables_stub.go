//go:build !linux

package nftables

import (
	"context"
	"fmt"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Enforcer is not supported on non-Linux platforms.
type Enforcer struct{}

func New(Config) (*Enforcer, error) {
	return nil, fmt.Errorf("nftables backend is only supported on linux")
}

func (e *Enforcer) Apply(context.Context, policy.PolicySpec) error {
	return fmt.Errorf("nftables backend not supported on this platform")
}

func (e *Enforcer) Remove(context.Context, string) error {
	return fmt.Errorf("nftables backend not supported on this platform")
}

func (e *Enforcer) List(context.Context) ([]policy.PolicySpec, error) {
	return nil, fmt.Errorf("nftables backend not supported on this platform")
}

func (e *Enforcer) HealthCheck(context.Context) error {
	return fmt.Errorf("nftables backend not supported on this platform")
}

func (e *Enforcer) ReadyCheck(ctx context.Context) error {
	return e.HealthCheck(ctx)
}
