//go:build linux

package iptables

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/enforcer/common"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Enforcer applies policies by managing iptables rules.
type Enforcer struct {
	bin    string
	dryRun bool
	log    Logger
	sets   selectorConfig
	runCmd commandRunner
	hooks  *testHooks

	mu    sync.Mutex
	rules map[string][]rule
	specs map[string]policy.PolicySpec
}

type commandRunner func(ctx context.Context, args ...string) error

type testHooks struct {
	beforeEnsure func(int, rule) error
	beforeDelete func(int, rule)
}

// New constructs an iptables enforcement backend.
func New(cfg Config) (*Enforcer, error) {
	bin := strings.TrimSpace(cfg.Binary)
	if bin == "" {
		bin = "iptables"
	}
	enf := &Enforcer{
		bin:    bin,
		dryRun: cfg.DryRun,
		log:    cfg.Logger,
		sets: selectorConfig{
			namespacePrefix: strings.TrimSpace(cfg.NamespaceSetPrefix),
			nodePrefix:      strings.TrimSpace(cfg.NodeSetPrefix),
		},
		rules: make(map[string][]rule),
		specs: make(map[string]policy.PolicySpec),
	}
	enf.runCmd = enf.run
	return enf, nil
}

// Apply materialises drop/reject rules for the given policy.
func (e *Enforcer) Apply(ctx context.Context, spec policy.PolicySpec) error {
	if err := common.ValidateScope(spec); err != nil {
		return err
	}

	rules, err := buildRules(spec, e.sets)
	if err != nil {
		return err
	}

	dryRun := e.dryRun || spec.Guardrails.DryRun

	e.mu.Lock()
	defer e.mu.Unlock()

	applied := make([]rule, 0, len(rules))
	for idx, r := range rules {
		if e.hooks != nil && e.hooks.beforeEnsure != nil {
			if err := e.hooks.beforeEnsure(idx, r); err != nil {
				for i := len(applied) - 1; i >= 0; i-- {
					if e.hooks != nil && e.hooks.beforeDelete != nil {
						e.hooks.beforeDelete(i, applied[i])
					}
					rollErr := e.deleteRule(ctx, applied[i], dryRun)
					if rollErr != nil && e.log != nil {
						e.log.Warn("iptables rollback failed", zap.String("policy_id", spec.ID), zap.Error(rollErr))
					}
				}
				return fmt.Errorf("iptables: apply policy %s: %w", spec.ID, err)
			}
		}

		added, err := e.ensureRule(ctx, r, dryRun)
		if err != nil {
			for i := len(applied) - 1; i >= 0; i-- {
				if e.hooks != nil && e.hooks.beforeDelete != nil {
					e.hooks.beforeDelete(i, applied[i])
				}
				rollErr := e.deleteRule(ctx, applied[i], dryRun)
				if rollErr != nil && e.log != nil {
					e.log.Warn("iptables rollback failed", zap.String("policy_id", spec.ID), zap.Error(rollErr))
				}
			}
			return fmt.Errorf("iptables: apply policy %s: %w", spec.ID, err)
		}
		if added {
			applied = append(applied, r)
		}
	}

	e.rules[spec.ID] = rules
	e.specs[spec.ID] = spec
	return nil
}

// Remove deletes rules previously installed for the policy.
func (e *Enforcer) Remove(ctx context.Context, policyID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	rules, ok := e.rules[policyID]
	if !ok {
		delete(e.specs, policyID)
		return nil
	}

	var dryRun bool
	if spec, ok := e.specs[policyID]; ok {
		dryRun = e.dryRun || spec.Guardrails.DryRun
	}

	var errs []string
	for idx, r := range rules {
		if e.hooks != nil && e.hooks.beforeDelete != nil {
			e.hooks.beforeDelete(idx, r)
		}
		if err := e.deleteRule(ctx, r, dryRun); err != nil {
			errs = append(errs, err.Error())
		}
	}

	delete(e.rules, policyID)
	delete(e.specs, policyID)

	if len(errs) > 0 {
		return fmt.Errorf("iptables: remove policy %s: %s", policyID, strings.Join(errs, "; "))
	}
	return nil
}

// List returns policies currently tracked in memory.
func (e *Enforcer) List(_ context.Context) ([]policy.PolicySpec, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	result := make([]policy.PolicySpec, 0, len(e.specs))
	for _, spec := range e.specs {
		result = append(result, spec)
	}
	return result, nil
}

func (e *Enforcer) ensureRule(ctx context.Context, r rule, dryRun bool) (bool, error) {
	if dryRun {
		e.logInfo("dry-run: would apply iptables rule", r)
		return false, nil
	}

	checkArgs := append([]string{"-C", r.Chain}, append(r.Args, "-j", r.Action)...)
	if err := e.runCmd(ctx, checkArgs...); err == nil {
		if e.log != nil {
			e.log.Info("iptables rule already present", zap.String("chain", r.Chain), zap.Strings("args", r.Args), zap.String("action", r.Action))
		}
		return false, nil
	}

	addArgs := append([]string{"-I", r.Chain}, append(r.Args, "-j", r.Action)...)
	if err := e.runCmd(ctx, addArgs...); err != nil {
		return false, err
	}
	if e.log != nil {
		e.log.Info("iptables rule added", zap.String("policy_action", r.Action), zap.String("chain", r.Chain), zap.Strings("args", r.Args))
	}
	return true, nil
}

func (e *Enforcer) deleteRule(ctx context.Context, r rule, dryRun bool) error {
	if dryRun {
		e.logInfo("dry-run: would delete iptables rule", r)
		return nil
	}

	delArgs := append([]string{"-D", r.Chain}, append(r.Args, "-j", r.Action)...)
	if err := e.runCmd(ctx, delArgs...); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Rule already absent; treat as success.
			return nil
		}
		return err
	}
	return nil
}

func (e *Enforcer) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, e.bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if e.log != nil {
			e.log.Error("iptables command failed", zap.String("binary", e.bin), zap.Strings("args", args), zap.ByteString("output", output), zap.Error(err))
		}
		return fmt.Errorf("%s %s: %w", e.bin, strings.Join(args, " "), err)
	}
	if e.log != nil {
		e.log.Debug("iptables command", zap.String("binary", e.bin), zap.Strings("args", args))
	}
	return nil
}

func (e *Enforcer) logInfo(msg string, r rule) {
	if e.log == nil {
		return
	}
	e.log.Info(msg,
		zap.String("chain", r.Chain),
		zap.Strings("args", r.Args),
		zap.String("action", r.Action),
		zap.Bool("dry_run", true),
		zap.Time("ts", time.Now().UTC()),
	)
}

// test helpers (not exported) -------------------------------------------------

func (e *Enforcer) setCommandRunner(r commandRunner) {
	if r == nil {
		e.runCmd = e.run
		return
	}
	e.runCmd = r
}

func (e *Enforcer) setTestHooks(h *testHooks) {
	e.hooks = h
}

// HealthCheck verifies that the iptables binary is reachable and responsive.
func (e *Enforcer) HealthCheck(ctx context.Context) error {
	if _, err := exec.LookPath(e.bin); err != nil {
		return fmt.Errorf("iptables: binary not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, e.bin, "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables: version check failed: %w", err)
	}
	return nil
}

// ReadyCheck delegates to HealthCheck for iptables backend.
func (e *Enforcer) ReadyCheck(ctx context.Context) error {
	return e.HealthCheck(ctx)
}
