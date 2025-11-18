//go:build !linux

package iptables

import (
	"context"
	"fmt"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Enforcer is a stub for non-Linux platforms.
type Enforcer struct{}

// New returns an error since iptables is Linux-only.
func New(Config) (*Enforcer, error) {
	return nil, fmt.Errorf("iptables backend is only supported on linux")
}

func (e *Enforcer) Apply(context.Context, policy.PolicySpec) error {
	return fmt.Errorf("iptables backend not supported on this platform")
}

func (e *Enforcer) Remove(context.Context, string) error {
	return fmt.Errorf("iptables backend not supported on this platform")
}

func (e *Enforcer) List(context.Context) ([]policy.PolicySpec, error) {
	return nil, fmt.Errorf("iptables backend not supported on this platform")
}

func (e *Enforcer) HealthCheck(context.Context) error {
	return fmt.Errorf("iptables backend not supported on this platform")
}

func (e *Enforcer) ReadyCheck(ctx context.Context) error {
	return e.HealthCheck(ctx)
}
