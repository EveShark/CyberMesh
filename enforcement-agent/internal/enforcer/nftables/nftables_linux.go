//go:build linux

package nftables

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/enforcer/common"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

type ruleReference struct {
	Chain  string
	Handle uint64
}

type commandRunner func(ctx context.Context, args ...string) error

type testHooks struct {
	beforeApply  func(int) error
	beforeDelete func(int)
}

type lookupFunc func(ctx context.Context, chain, comment string) (uint64, error)

type selectorConfig struct {
	namespacePrefix string
	nodePrefix      string
}

// Enforcer manipulates nft tables to apply block rules.
type Enforcer struct {
	bin      string
	dryRun   bool
	log      *zap.Logger
	sets     selectorConfig
	runCmd   commandRunner
	hooks    *testHooks
	lookupFn lookupFunc

	mu    sync.Mutex
	rules map[string][]ruleReference
	specs map[string]policy.PolicySpec
}

// New creates the nftables backend.
func New(cfg Config) (*Enforcer, error) {
	bin := strings.TrimSpace(cfg.Binary)
	if bin == "" {
		bin = "nft"
	}

	e := &Enforcer{
		bin:    bin,
		dryRun: cfg.DryRun,
		log:    cfg.Logger,
		sets: selectorConfig{
			namespacePrefix: strings.TrimSpace(cfg.NamespaceSetPrefix),
			nodePrefix:      strings.TrimSpace(cfg.NodeSetPrefix),
		},
		rules: make(map[string][]ruleReference),
		specs: make(map[string]policy.PolicySpec),
	}
	e.runCmd = e.run
	e.lookupFn = e.lookupHandleExec

	if err := e.ensureBaseChains(context.Background()); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Enforcer) ensureBaseChains(ctx context.Context) error {
	if e.dryRun {
		return nil
	}
	commands := [][]string{
		{"list", "table", "inet", "filter"},
	}
	if err := e.runCmd(ctx, commands[0]...); err == nil {
		return nil
	}
	setup := []string{
		"add", "table", "inet", "filter",
	}
	if err := e.runCmd(ctx, setup...); err != nil {
		return fmt.Errorf("nftables: create table: %w", err)
	}
	for _, chain := range [][]string{
		{"add", "chain", "inet", "filter", "ingress", "{", "type", "filter", "hook", "input", "priority", "0", ";", "policy", "accept", "}"},
		{"add", "chain", "inet", "filter", "egress", "{", "type", "filter", "hook", "output", "priority", "0", ";", "policy", "accept", "}"},
		{"add", "chain", "inet", "filter", "forward", "{", "type", "filter", "hook", "forward", "priority", "0", ";", "policy", "accept", "}"},
	} {
		if err := e.runCmd(ctx, chain...); err != nil {
			return fmt.Errorf("nftables: create chain: %w", err)
		}
	}
	return nil
}

// Apply installs drop/reject rules for the policy.
func (e *Enforcer) Apply(ctx context.Context, spec policy.PolicySpec) error {
	if err := common.ValidateScope(spec); err != nil {
		return err
	}

	targets := append([]string{}, spec.Target.IPs...)
	targets = append(targets, spec.Target.CIDRs...)
	if len(targets) == 0 {
		if len(spec.Target.Selectors) == 0 {
			return fmt.Errorf("nftables: policy %s has no targets", spec.ID)
		}
		setNames, err := buildSelectorSets(spec, e.sets)
		if err != nil {
			return err
		}
		for _, setName := range setNames {
			targets = append(targets, "@"+setName)
		}
	}

	action := strings.ToLower(strings.TrimSpace(spec.Action))
	if action == "" {
		action = "drop"
	}
	if action != "drop" && action != "reject" {
		return fmt.Errorf("nftables: unsupported action %s", spec.Action)
	}

	dryRun := e.dryRun || spec.Guardrails.DryRun
	descriptors := directionDescriptors(spec.Target.Direction)
	if len(descriptors) == 0 {
		return fmt.Errorf("nftables: unsupported direction %s", spec.Target.Direction)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	protocols := normaliseProtocols(spec.Target.Protocols, len(spec.Target.Ports) > 0)
	if len(protocols) == 0 {
		protocols = []string{""}
	}

	portRanges := spec.Target.Ports
	if len(portRanges) == 0 {
		portRanges = []policy.PortRange{{From: 0, To: 0}}
	}

	var refs []ruleReference
	applied := make([]ruleReference, 0)
	ruleIdx := 0
	for _, desc := range descriptors {
		for _, target := range targets {
			for _, proto := range protocols {
				protoLower := strings.ToLower(proto)
				requiresPort := len(spec.Target.Ports) > 0
				if requiresPort && protoLower != "tcp" && protoLower != "udp" && protoLower != "sctp" && protoLower != "udplite" {
					return fmt.Errorf("nftables: ports specified but protocol %s does not support ports", proto)
				}
				for _, pr := range portRanges {
					if e.hooks != nil && e.hooks.beforeApply != nil {
						if err := e.hooks.beforeApply(ruleIdx); err != nil {
							for i := len(applied) - 1; i >= 0; i-- {
								if e.hooks != nil && e.hooks.beforeDelete != nil {
									e.hooks.beforeDelete(i)
								}
								if rollErr := e.deleteRule(ctx, applied[i], dryRun); rollErr != nil && e.log != nil {
									e.log.Warn("nftables rollback failed", zap.String("policy_id", spec.ID), zap.Error(rollErr))
								}
							}
							return fmt.Errorf("nftables: apply policy %s: %w", spec.ID, err)
						}
					}
					ref, err := e.applyRule(ctx, spec.ID, desc, target, action, protoLower, pr, dryRun)
					if err != nil {
						for i := len(applied) - 1; i >= 0; i-- {
							if e.hooks != nil && e.hooks.beforeDelete != nil {
								e.hooks.beforeDelete(i)
							}
							if rollErr := e.deleteRule(ctx, applied[i], dryRun); rollErr != nil && e.log != nil {
								e.log.Warn("nftables rollback failed", zap.String("policy_id", spec.ID), zap.Error(rollErr))
							}
						}
						return fmt.Errorf("nftables: apply policy %s: %w", spec.ID, err)
					}
					if ref != nil {
						refs = append(refs, *ref)
						if !dryRun {
							applied = append(applied, *ref)
						}
					}
					ruleIdx++
				}
			}
		}
	}

	e.rules[spec.ID] = refs
	e.specs[spec.ID] = spec
	return nil
}

// Remove deletes rules installed for the policy.
func (e *Enforcer) Remove(ctx context.Context, policyID string) error {
	e.mu.Lock()
	refs := e.rules[policyID]
	spec, ok := e.specs[policyID]
	e.mu.Unlock()

	dryRun := e.dryRun
	if ok {
		dryRun = dryRun || spec.Guardrails.DryRun
	}

	var errs []string
	for idx, ref := range refs {
		if e.hooks != nil && e.hooks.beforeDelete != nil {
			e.hooks.beforeDelete(idx)
		}
		if err := e.deleteRule(ctx, ref, dryRun); err != nil {
			errs = append(errs, err.Error())
		}
	}

	e.mu.Lock()
	delete(e.rules, policyID)
	delete(e.specs, policyID)
	e.mu.Unlock()

	if len(errs) > 0 {
		return fmt.Errorf("nftables: remove policy %s: %s", policyID, strings.Join(errs, "; "))
	}
	return nil
}

// List returns policies tracked by the backend.
func (e *Enforcer) List(context.Context) ([]policy.PolicySpec, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	res := make([]policy.PolicySpec, 0, len(e.specs))
	for _, spec := range e.specs {
		res = append(res, spec)
	}
	return res, nil
}

type directionDescriptor struct {
	Chain string
	Kind  string
	Field string
}

func directionDescriptors(direction string) []directionDescriptor {
	dir := strings.ToLower(strings.TrimSpace(direction))
	switch dir {
	case "", "any", "both":
		return []directionDescriptor{
			{Chain: "ingress", Kind: "ip", Field: "saddr"},
			{Chain: "egress", Kind: "ip", Field: "daddr"},
		}
	case "ingress", "inbound":
		return []directionDescriptor{{Chain: "ingress", Kind: "ip", Field: "saddr"}}
	case "egress", "outbound":
		return []directionDescriptor{{Chain: "egress", Kind: "ip", Field: "daddr"}}
	default:
		return nil
	}
}

func (e *Enforcer) applyRule(ctx context.Context, policyID string, desc directionDescriptor, target, action, proto string, port policy.PortRange, dryRun bool) (*ruleReference, error) {
	comment := commentFor(policyID, target, desc.Chain)
	args := []string{"add", "rule", "inet", "filter", desc.Chain}
	if strings.Contains(target, ":") {
		args = append(args, "ip6", desc.Field, target)
	} else {
		args = append(args, desc.Kind, desc.Field, target)
	}
	if proto != "" && proto != "any" {
		args = append(args, "meta", "l4proto", proto)
	}
	if port.From > 0 {
		if proto == "" || proto == "any" {
			return nil, fmt.Errorf("nftables: port range requires transport protocol")
		}
		portArg := fmt.Sprintf("%d", port.From)
		if port.To > port.From {
			portArg = fmt.Sprintf("%d-%d", port.From, port.To)
		}
		args = append(args, proto, "dport", portArg)
	}
	args = append(args, "counter", action, "comment", comment)

	if dryRun {
		e.logRule("dry-run: nft add rule", desc.Chain, target, action, comment)
		return &ruleReference{Chain: desc.Chain, Handle: 0}, nil
	}

	if err := e.runCmd(ctx, args...); err != nil {
		return nil, err
	}
	if e.log != nil {
		e.log.Info("nftables rule added", zap.String("chain", desc.Chain), zap.String("target", target), zap.String("action", action), zap.String("comment", comment))
	}

	handle, err := e.lookupFn(ctx, desc.Chain, comment)
	if err != nil {
		return nil, err
	}
	return &ruleReference{Chain: desc.Chain, Handle: handle}, nil
}

func (e *Enforcer) deleteRule(ctx context.Context, ref ruleReference, dryRun bool) error {
	if dryRun {
		if e.log != nil {
			e.log.Info("dry-run: nft delete rule",
				zap.String("chain", ref.Chain),
				zap.Uint64("handle", ref.Handle),
				zap.Time("ts", time.Now().UTC()),
			)
		}
		return nil
	}
	if ref.Handle == 0 {
		return nil
	}
	args := []string{"delete", "rule", "inet", "filter", ref.Chain, "handle", fmt.Sprintf("%d", ref.Handle)}
	if err := e.runCmd(ctx, args...); err != nil {
		return err
	}
	if e.log != nil {
		e.log.Info("nftables rule removed", zap.String("chain", ref.Chain), zap.Uint64("handle", ref.Handle))
	}
	return nil
}

func (e *Enforcer) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, e.bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if e.log != nil {
			e.log.Error("nft command failed", zap.String("binary", e.bin), zap.Strings("args", args), zap.ByteString("output", output), zap.Error(err))
		}
		return fmt.Errorf("%s %s: %w", e.bin, strings.Join(args, " "), err)
	}
	if e.log != nil {
		e.log.Debug("nft command", zap.String("binary", e.bin), zap.Strings("args", args))
	}
	return nil
}

func (e *Enforcer) lookupHandleExec(ctx context.Context, chain, comment string) (uint64, error) {
	cmd := exec.CommandContext(ctx, e.bin, "--json", "list", "chain", "inet", "filter", chain)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("nft list chain %s: %w", chain, err)
	}
	var dump nftDump
	if err := json.Unmarshal(output, &dump); err != nil {
		return 0, fmt.Errorf("nft json parse: %w", err)
	}
	for _, entry := range dump.NFTables {
		if entry.Rule != nil && entry.Rule.Comment == comment {
			return entry.Rule.Handle, nil
		}
	}
	return 0, fmt.Errorf("nft: rule with comment %s not found", comment)
}

func (e *Enforcer) logRule(msg, chain, target, action, comment string) {
	if e.log == nil {
		return
	}
	e.log.Info(msg,
		zap.String("chain", chain),
		zap.String("target", target),
		zap.String("action", action),
		zap.String("comment", comment),
		zap.Bool("dry_run", true),
		zap.Time("ts", time.Now().UTC()),
	)
}

// HealthCheck verifies nft binary availability.
func (e *Enforcer) HealthCheck(ctx context.Context) error {
	if _, err := exec.LookPath(e.bin); err != nil {
		return fmt.Errorf("nftables: binary not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, e.bin, "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nftables: version check failed: %w", err)
	}
	return nil
}

// ReadyCheck delegates to HealthCheck for nftables backend.
func (e *Enforcer) ReadyCheck(ctx context.Context) error {
	return e.HealthCheck(ctx)
}

// test helpers ---------------------------------------------------------------

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

func (e *Enforcer) setLookupFunc(l lookupFunc) {
	if l == nil {
		e.lookupFn = e.lookupHandleExec
		return
	}
	e.lookupFn = l
}

type nftDump struct {
	NFTables []nftObject `json:"nftables"`
}

type nftObject struct {
	Rule *nftRule `json:"rule"`
}

type nftRule struct {
	Handle  uint64 `json:"handle"`
	Comment string `json:"comment"`
}

func commentFor(policyID, target, chain string) string {
	h := sha1.Sum([]byte(target + chain))
	return fmt.Sprintf("cm:%s:%s", policyID, hex.EncodeToString(h[:4]))
}

func buildSelectorSets(spec policy.PolicySpec, cfg selectorConfig) ([]string, error) {
	sets := make([]string, 0, 2)
	namespace := strings.TrimSpace(spec.Target.Namespace)
	if namespace == "" {
		if v, ok := spec.Target.Selectors["namespace"]; ok {
			namespace = strings.TrimSpace(v)
		}
	}
	if namespace != "" {
		if cfg.namespacePrefix == "" {
			return nil, fmt.Errorf("nftables: namespace selector requires NamespaceSetPrefix")
		}
		sets = append(sets, sanitizeSetName(cfg.namespacePrefix, namespace))
	}

	node := strings.TrimSpace(spec.Target.Selectors["node"])
	if node != "" {
		if cfg.nodePrefix == "" {
			return nil, fmt.Errorf("nftables: node selector requires NodeSetPrefix")
		}
		sets = append(sets, sanitizeSetName(cfg.nodePrefix, node))
	}

	if len(sets) == 0 {
		return nil, fmt.Errorf("nftables: selector-only policy %s unsupported", spec.ID)
	}

	unique := make(map[string]struct{}, len(sets))
	result := make([]string, 0, len(sets))
	for _, set := range sets {
		if _, ok := unique[set]; ok {
			continue
		}
		unique[set] = struct{}{}
		result = append(result, set)
	}
	return result, nil
}

func sanitizeSetName(prefix, value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	runes := make([]rune, 0, len(trimmed))
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			runes = append(runes, r)
			continue
		}
		runes = append(runes, '_')
	}
	return prefix + string(runes)
}

func normaliseProtocols(protocols []string, portsDefined bool) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(protocols))
	for _, proto := range protocols {
		upper := strings.ToUpper(strings.TrimSpace(proto))
		if upper == "" {
			continue
		}
		if upper == "ANY" {
			return []string{""}
		}
		if _, ok := seen[upper]; ok {
			continue
		}
		seen[upper] = struct{}{}
		result = append(result, upper)
	}
	if len(result) == 0 {
		if portsDefined {
			return []string{"TCP", "UDP"}
		}
		return []string{""}
	}
	return result
}
