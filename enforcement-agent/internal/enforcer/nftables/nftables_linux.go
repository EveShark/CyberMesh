//go:build linux

package nftables

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
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
	bin         string
	dryRun      bool
	batchAtomic bool
	log         *zap.Logger
	sets        selectorConfig
	runCmd      commandRunner
	hooks       *testHooks
	lookupFn    lookupFunc

	mu    sync.Mutex
	rules map[string][]ruleReference
	specs map[string]policy.PolicySpec
	// Throttle best-effort handle lookup under burst.
	lastHandleLookupAt time.Time
	// installedByPolicy keeps currently-installed rule keys for each policy.
	installedByPolicy map[string]map[ruleKey]struct{}
	// installedByKey tracks handle metadata for each installed rule key.
	installedByKey map[ruleKey]ruleReference
}

const (
	// Keep lookup pressure bounded during burst; unresolved handles are retried later.
	handleLookupBudget    = 2 * time.Second
	handleLookupMinPeriod = 1 * time.Second
)

// New creates the nftables backend.
func New(cfg Config) (*Enforcer, error) {
	bin := strings.TrimSpace(cfg.Binary)
	if bin == "" {
		bin = "nft"
	}

	e := &Enforcer{
		bin:         bin,
		dryRun:      cfg.DryRun,
		batchAtomic: cfg.BatchAtomic,
		log:         cfg.Logger,
		sets: selectorConfig{
			namespacePrefix: strings.TrimSpace(cfg.NamespaceSetPrefix),
			nodePrefix:      strings.TrimSpace(cfg.NodeSetPrefix),
		},
		rules:             make(map[string][]ruleReference),
		specs:             make(map[string]policy.PolicySpec),
		installedByPolicy: make(map[string]map[ruleKey]struct{}),
		installedByKey:    make(map[ruleKey]ruleReference),
	}
	e.runCmd = e.run
	e.lookupFn = e.lookupHandleExec

	if err := e.ensureBaseChains(context.Background()); err != nil {
		return nil, err
	}
	if !e.dryRun {
		if err := e.bootstrapInstalledRules(context.Background()); err != nil {
			return nil, err
		}
	}
	return e, nil
}

func (e *Enforcer) ensureBaseChains(ctx context.Context) error {
	if e.dryRun {
		return nil
	}

	// Ensure table exists.
	if err := e.runCmd(ctx, "list", "table", "inet", "filter"); err != nil {
		if err := e.runCmd(ctx, "add", "table", "inet", "filter"); err != nil {
			return fmt.Errorf("nftables: create table: %w", err)
		}
	}

	// Ensure expected base chains exist. A previous run may have created the table
	// but not the chains, and adding rules would then fail.
	type chainSpec struct {
		name string
		hook string
	}
	for _, spec := range []chainSpec{
		{name: "ingress", hook: "input"},
		{name: "egress", hook: "output"},
		{name: "forward", hook: "forward"},
	} {
		if err := e.runCmd(ctx, "list", "chain", "inet", "filter", spec.name); err == nil {
			continue
		}
		chain := []string{
			"add", "chain", "inet", "filter", spec.name,
			"{", "type", "filter", "hook", spec.hook, "priority", "0", ";", "policy", "accept", ";", "}",
		}
		if err := e.runCmd(ctx, chain...); err != nil {
			return fmt.Errorf("nftables: create chain %s: %w", spec.name, err)
		}
	}
	return nil
}

var (
	reCommentHandle = regexp.MustCompile(`comment\s+"([^"]+)"\s+#\s+handle\s+([0-9]+)`)
	reAddrMatch     = regexp.MustCompile(`\bip6?\s+(saddr|daddr)\s+([^\s]+)`)
	reProtoMatch    = regexp.MustCompile(`\bmeta\s+l4proto\s+([a-z0-9]+)`)
	reDPortMatch    = regexp.MustCompile(`\bdport\s+([0-9]+)(?:-([0-9]+))?`)
)

func (e *Enforcer) bootstrapInstalledRules(ctx context.Context) error {
	chains := []string{"ingress", "egress", "forward"}
	e.mu.Lock()
	e.ensureStateMapsLocked()
	e.installedByKey = make(map[ruleKey]ruleReference)
	e.installedByPolicy = make(map[string]map[ruleKey]struct{})
	e.rules = make(map[string][]ruleReference)
	e.mu.Unlock()

	for _, chain := range chains {
		out, err := e.listChainWithHandles(ctx, chain)
		if err != nil {
			return fmt.Errorf("nftables: bootstrap list chain %s failed: %w", chain, err)
		}
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			key, ref, ok := parseManagedRuleLine(chain, line)
			if !ok {
				continue
			}
			e.mu.Lock()
			e.ensureStateMapsLocked()
			e.installedByKey[key] = ref
			if _, exists := e.installedByPolicy[key.policyID]; !exists {
				e.installedByPolicy[key.policyID] = make(map[ruleKey]struct{})
			}
			e.installedByPolicy[key.policyID][key] = struct{}{}
			e.mu.Unlock()
		}
	}

	e.mu.Lock()
	for policyID := range e.installedByPolicy {
		e.refreshPolicyRefsLocked(policyID)
	}
	e.mu.Unlock()
	return nil
}

func (e *Enforcer) listChainWithHandles(ctx context.Context, chain string) (string, error) {
	cmd := exec.CommandContext(ctx, e.bin, "-a", "list", "chain", "inet", "filter", chain)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("nft -a list chain %s: %w", chain, err)
	}
	return string(output), nil
}

func parseManagedRuleLine(chain, line string) (ruleKey, ruleReference, bool) {
	m := reCommentHandle.FindStringSubmatch(line)
	if len(m) != 3 {
		return ruleKey{}, ruleReference{}, false
	}
	comment := strings.TrimSpace(m[1])
	policyID, ok := policyIDFromComment(comment)
	if !ok {
		return ruleKey{}, ruleReference{}, false
	}
	handle, err := strconv.ParseUint(strings.TrimSpace(m[2]), 10, 64)
	if err != nil {
		return ruleKey{}, ruleReference{}, false
	}

	addr := reAddrMatch.FindStringSubmatch(line)
	if len(addr) != 3 {
		return ruleKey{}, ruleReference{}, false
	}
	target := strings.TrimSpace(addr[2])

	action := ""
	switch {
	case strings.Contains(line, " counter drop"):
		action = "drop"
	case strings.Contains(line, " counter reject"):
		action = "reject"
	default:
		return ruleKey{}, ruleReference{}, false
	}

	proto := ""
	if pm := reProtoMatch.FindStringSubmatch(line); len(pm) == 2 {
		proto = strings.TrimSpace(pm[1])
	}

	var from, to uint16
	if dm := reDPortMatch.FindStringSubmatch(line); len(dm) >= 2 {
		pf, err := strconv.ParseUint(strings.TrimSpace(dm[1]), 10, 16)
		if err != nil {
			return ruleKey{}, ruleReference{}, false
		}
		from = uint16(pf)
		to = from
		if len(dm) == 3 && strings.TrimSpace(dm[2]) != "" {
			pt, err := strconv.ParseUint(strings.TrimSpace(dm[2]), 10, 16)
			if err != nil {
				return ruleKey{}, ruleReference{}, false
			}
			to = uint16(pt)
		}
	}

	key := ruleKey{
		policyID: policyID,
		chain:    chain,
		target:   target,
		action:   action,
		proto:    proto,
		portFrom: from,
		portTo:   to,
	}
	return key, ruleReference{Chain: chain, Handle: handle}, true
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
	desired, err := e.buildDesiredRules(spec, targets, action)
	if err != nil {
		return err
	}
	if err := e.reconcilePolicyRules(ctx, spec.ID, desired, dryRun); err != nil {
		return fmt.Errorf("nftables: apply policy %s: %w", spec.ID, err)
	}

	e.mu.Lock()
	e.ensureStateMapsLocked()
	e.specs[spec.ID] = spec
	e.refreshPolicyRefsLocked(spec.ID)
	e.mu.Unlock()
	return nil
}

// Remove deletes rules installed for the policy.
func (e *Enforcer) Remove(ctx context.Context, policyID string) error {
	e.mu.Lock()
	spec, ok := e.specs[policyID]
	e.mu.Unlock()

	dryRun := e.dryRun
	if ok {
		dryRun = dryRun || spec.Guardrails.DryRun
	}
	if err := e.reconcilePolicyRules(ctx, policyID, nil, dryRun); err != nil {
		return fmt.Errorf("nftables: remove policy %s: %w", policyID, err)
	}

	e.mu.Lock()
	e.ensureStateMapsLocked()
	delete(e.specs, policyID)
	delete(e.rules, policyID)
	delete(e.installedByPolicy, policyID)
	e.mu.Unlock()
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

type ruleKey struct {
	policyID string
	chain    string
	target   string
	action   string
	proto    string
	portFrom uint16
	portTo   uint16
}

type desiredRule struct {
	key     ruleKey
	desc    directionDescriptor
	target  string
	action  string
	proto   string
	port    policy.PortRange
	comment string
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

func (e *Enforcer) applyRule(ctx context.Context, desc directionDescriptor, target, action, proto string, port policy.PortRange, comment string, dryRun bool) (*ruleReference, error) {
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

type batchedRule struct {
	desc    directionDescriptor
	target  string
	action  string
	proto   string
	port    policy.PortRange
	comment string
}

func (e *Enforcer) applyRulesBatch(ctx context.Context, rules []batchedRule) ([]ruleReference, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	lines := make([]string, 0, len(rules))
	for _, r := range rules {
		line, err := renderAddRuleLine(r)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	if err := e.runBatch(ctx, lines); err != nil {
		return nil, err
	}
	refs := make([]ruleReference, 0, len(rules))
	for _, r := range rules {
		handle, err := e.lookupFn(ctx, r.desc.Chain, r.comment)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ruleReference{Chain: r.desc.Chain, Handle: handle})
	}
	return refs, nil
}

func renderAddRuleLine(r batchedRule) (string, error) {
	var b strings.Builder
	b.WriteString("add rule inet filter ")
	b.WriteString(r.desc.Chain)
	b.WriteString(" ")
	if strings.Contains(r.target, ":") {
		b.WriteString("ip6 ")
	} else {
		b.WriteString(r.desc.Kind)
		b.WriteString(" ")
	}
	b.WriteString(r.desc.Field)
	b.WriteString(" ")
	b.WriteString(r.target)
	if r.proto != "" && r.proto != "any" {
		b.WriteString(" meta l4proto ")
		b.WriteString(r.proto)
	}
	if r.port.From > 0 {
		if r.proto == "" || r.proto == "any" {
			return "", fmt.Errorf("nftables: port range requires transport protocol")
		}
		b.WriteString(" ")
		b.WriteString(r.proto)
		b.WriteString(" dport ")
		if r.port.To > r.port.From {
			b.WriteString(fmt.Sprintf("%d-%d", r.port.From, r.port.To))
		} else {
			b.WriteString(fmt.Sprintf("%d", r.port.From))
		}
	}
	b.WriteString(" counter ")
	b.WriteString(r.action)
	b.WriteString(" comment \"")
	b.WriteString(r.comment)
	b.WriteString("\"")
	return b.String(), nil
}

func (e *Enforcer) buildDesiredRules(spec policy.PolicySpec, targets []string, action string) ([]desiredRule, error) {
	descriptors := directionDescriptors(spec.Target.Direction)
	if len(descriptors) == 0 {
		return nil, fmt.Errorf("nftables: unsupported direction %s", spec.Target.Direction)
	}
	protocols := normaliseProtocols(spec.Target.Protocols, len(spec.Target.Ports) > 0)
	if len(protocols) == 0 {
		protocols = []string{""}
	}
	portRanges := spec.Target.Ports
	if len(portRanges) == 0 {
		portRanges = []policy.PortRange{{From: 0, To: 0}}
	}

	out := make([]desiredRule, 0)
	for _, desc := range descriptors {
		for _, target := range targets {
			for _, proto := range protocols {
				protoLower := strings.ToLower(proto)
				requiresPort := len(spec.Target.Ports) > 0
				if requiresPort && protoLower != "tcp" && protoLower != "udp" && protoLower != "sctp" && protoLower != "udplite" {
					return nil, fmt.Errorf("nftables: ports specified but protocol %s does not support ports", proto)
				}
				for _, pr := range portRanges {
					comment := commentFor(spec.ID, target, desc.Chain, action, protoLower, pr)
					key := ruleKey{
						policyID: strings.TrimSpace(spec.ID),
						chain:    desc.Chain,
						target:   target,
						action:   action,
						proto:    protoLower,
						portFrom: uint16(pr.From),
						portTo:   uint16(pr.To),
					}
					out = append(out, desiredRule{
						key:     key,
						desc:    desc,
						target:  target,
						action:  action,
						proto:   protoLower,
						port:    pr,
						comment: comment,
					})
				}
			}
		}
	}
	return out, nil
}

func (e *Enforcer) reconcilePolicyRules(ctx context.Context, policyID string, desired []desiredRule, dryRun bool) error {
	e.mu.Lock()
	e.ensureStateMapsLocked()
	existing := make(map[ruleKey]ruleReference)
	for k, ref := range e.installedByKey {
		if k.policyID == policyID {
			existing[k] = ref
		}
	}
	e.mu.Unlock()

	desiredByKey := make(map[ruleKey]desiredRule, len(desired))
	for _, rule := range desired {
		desiredByKey[rule.key] = rule
	}

	toDelete := make([]ruleKey, 0)
	toAdd := make([]desiredRule, 0)

	for key := range existing {
		if _, ok := desiredByKey[key]; !ok {
			toDelete = append(toDelete, key)
		}
	}
	for key, rule := range desiredByKey {
		if _, ok := existing[key]; !ok {
			toAdd = append(toAdd, rule)
		}
	}

	if len(toDelete) == 0 && len(toAdd) == 0 {
		e.mu.Lock()
		e.rebuildPolicyInstalledSetLocked(policyID, existing)
		e.refreshPolicyRefsLocked(policyID)
		e.mu.Unlock()
		return nil
	}

	if e.batchAtomic && !dryRun && e.hooks == nil {
		if err := e.reconcilePolicyBatch(ctx, policyID, toDelete, toAdd, desiredByKey, existing); err != nil {
			return err
		}
		return nil
	}
	if err := e.reconcilePolicyIterative(ctx, policyID, toDelete, toAdd, existing, dryRun); err != nil {
		return err
	}
	return nil
}

func (e *Enforcer) reconcilePolicyBatch(
	ctx context.Context,
	policyID string,
	toDelete []ruleKey,
	toAdd []desiredRule,
	desiredByKey map[ruleKey]desiredRule,
	existing map[ruleKey]ruleReference,
) error {
	e.tryResolveDeleteHandles(ctx, existing, toDelete)

	lines := make([]string, 0, len(toDelete)+len(toAdd))
	for _, key := range toDelete {
		ref := existing[key]
		if ref.Handle == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("delete rule inet filter %s handle %d", ref.Chain, ref.Handle))
	}
	for _, rule := range toAdd {
		line, err := renderAddRuleLine(batchedRule{
			desc:    rule.desc,
			target:  rule.target,
			action:  rule.action,
			proto:   rule.proto,
			port:    rule.port,
			comment: rule.comment,
		})
		if err != nil {
			return err
		}
		lines = append(lines, line)
	}
	if err := e.runBatch(ctx, lines); err != nil {
		return err
	}

	e.mu.Lock()
	e.ensureStateMapsLocked()
	for _, key := range toDelete {
		delete(e.installedByKey, key)
	}
	for _, rule := range toAdd {
		// Track as installed immediately; handle may resolve asynchronously.
		e.installedByKey[rule.key] = ruleReference{Chain: rule.desc.Chain, Handle: 0}
	}
	e.mu.Unlock()

	e.resolvePolicyHandlesBestEffort(ctx, policyID, desiredByKey)

	e.mu.Lock()
	e.ensureStateMapsLocked()
	next := make(map[ruleKey]ruleReference)
	for k, ref := range e.installedByKey {
		if k.policyID == policyID {
			next[k] = ref
		}
	}
	e.rebuildPolicyInstalledSetLocked(policyID, next)
	e.refreshPolicyRefsLocked(policyID)
	e.mu.Unlock()
	return nil
}

func (e *Enforcer) resolvePolicyHandlesBestEffort(ctx context.Context, policyID string, desiredByKey map[ruleKey]desiredRule) {
	if len(desiredByKey) == 0 {
		return
	}
	_ = ctx
	now := time.Now()
	e.mu.Lock()
	if !e.lastHandleLookupAt.IsZero() && now.Sub(e.lastHandleLookupAt) < handleLookupMinPeriod {
		e.mu.Unlock()
		return
	}
	e.lastHandleLookupAt = now
	e.mu.Unlock()

	lookupCtx, cancel := context.WithTimeout(context.Background(), handleLookupBudget)
	defer cancel()

	chainComments := make(map[string]map[string]struct{})
	for _, rule := range desiredByKey {
		if _, ok := chainComments[rule.desc.Chain]; !ok {
			chainComments[rule.desc.Chain] = make(map[string]struct{})
		}
		chainComments[rule.desc.Chain][rule.comment] = struct{}{}
	}
	matches, err := e.lookupHandlesByChainSnapshot(lookupCtx, chainComments)
	if err != nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureStateMapsLocked()
	for key, rule := range desiredByKey {
		ref := e.installedByKey[key]
		if ref.Handle != 0 {
			continue
		}
		if byChain, ok := matches[rule.desc.Chain]; ok {
			if h, ok := byChain[rule.comment]; ok {
				ref.Chain = rule.desc.Chain
				ref.Handle = h
				e.installedByKey[key] = ref
			}
		}
	}
	e.refreshPolicyRefsLocked(policyID)
}

func (e *Enforcer) tryResolveDeleteHandles(ctx context.Context, existing map[ruleKey]ruleReference, toDelete []ruleKey) {
	_ = ctx
	unresolved := make(map[string]map[string]struct{})
	for _, key := range toDelete {
		ref := existing[key]
		if ref.Handle != 0 {
			continue
		}
		comment := commentForKey(key)
		chain := key.chain
		if chain == "" {
			continue
		}
		if _, ok := unresolved[chain]; !ok {
			unresolved[chain] = make(map[string]struct{})
		}
		unresolved[chain][comment] = struct{}{}
	}
	if len(unresolved) == 0 {
		return
	}
	lookupCtx, cancel := context.WithTimeout(context.Background(), handleLookupBudget)
	defer cancel()
	matches, err := e.lookupHandlesByChainSnapshot(lookupCtx, unresolved)
	if err != nil {
		return
	}
	for _, key := range toDelete {
		ref := existing[key]
		if ref.Handle != 0 {
			continue
		}
		comment := commentForKey(key)
		if byChain, ok := matches[key.chain]; ok {
			if h, ok := byChain[comment]; ok {
				ref.Handle = h
				ref.Chain = key.chain
				existing[key] = ref
			}
		}
	}
}

func (e *Enforcer) lookupHandlesByChainSnapshot(ctx context.Context, chainComments map[string]map[string]struct{}) (map[string]map[string]uint64, error) {
	out := make(map[string]map[string]uint64, len(chainComments))
	for chain, comments := range chainComments {
		if len(comments) == 0 {
			continue
		}
		raw, err := e.listChainWithHandles(ctx, chain)
		if err != nil {
			return nil, err
		}
		lineMatches := parseChainCommentHandles(raw)
		hits := make(map[string]uint64)
		for comment := range comments {
			if h, ok := lineMatches[comment]; ok {
				hits[comment] = h
			}
		}
		out[chain] = hits
	}
	return out, nil
}

func parseChainCommentHandles(raw string) map[string]uint64 {
	out := make(map[string]uint64)
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		m := reCommentHandle.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		comment := strings.TrimSpace(m[1])
		handle, err := strconv.ParseUint(strings.TrimSpace(m[2]), 10, 64)
		if err != nil {
			continue
		}
		// Keep first match deterministically.
		if _, exists := out[comment]; !exists {
			out[comment] = handle
		}
	}
	return out
}

func commentForKey(key ruleKey) string {
	return commentFor(
		key.policyID,
		key.target,
		key.chain,
		key.action,
		key.proto,
		policy.PortRange{From: int(key.portFrom), To: int(key.portTo)},
	)
}

func (e *Enforcer) reconcilePolicyIterative(ctx context.Context, policyID string, toDelete []ruleKey, toAdd []desiredRule, existing map[ruleKey]ruleReference, dryRun bool) error {
	appliedAdds := make([]ruleKey, 0)

	for idx, key := range toDelete {
		ref := existing[key]
		if e.hooks != nil && e.hooks.beforeDelete != nil {
			e.hooks.beforeDelete(idx)
		}
		if err := e.deleteRule(ctx, ref, dryRun); err != nil {
			return err
		}
		e.mu.Lock()
		e.ensureStateMapsLocked()
		delete(e.installedByKey, key)
		e.mu.Unlock()
	}

	for idx, rule := range toAdd {
		if e.hooks != nil && e.hooks.beforeApply != nil {
			if err := e.hooks.beforeApply(idx); err != nil {
				for i := len(appliedAdds) - 1; i >= 0; i-- {
					ref := e.installedByKey[appliedAdds[i]]
					if e.hooks != nil && e.hooks.beforeDelete != nil {
						e.hooks.beforeDelete(i)
					}
					if rollErr := e.deleteRule(ctx, ref, dryRun); rollErr != nil && e.log != nil {
						e.log.Warn("nftables rollback failed", zap.String("policy_id", policyID), zap.Error(rollErr))
					}
					e.mu.Lock()
					e.ensureStateMapsLocked()
					delete(e.installedByKey, appliedAdds[i])
					e.mu.Unlock()
				}
				return err
			}
		}

		ref, err := e.applyRule(ctx, rule.desc, rule.target, rule.action, rule.proto, rule.port, rule.comment, dryRun)
		if err != nil {
			for i := len(appliedAdds) - 1; i >= 0; i-- {
				prevRef := e.installedByKey[appliedAdds[i]]
				if e.hooks != nil && e.hooks.beforeDelete != nil {
					e.hooks.beforeDelete(i)
				}
				if rollErr := e.deleteRule(ctx, prevRef, dryRun); rollErr != nil && e.log != nil {
					e.log.Warn("nftables rollback failed", zap.String("policy_id", policyID), zap.Error(rollErr))
				}
				e.mu.Lock()
				e.ensureStateMapsLocked()
				delete(e.installedByKey, appliedAdds[i])
				e.mu.Unlock()
			}
			return err
		}
		if ref != nil {
			e.mu.Lock()
			e.ensureStateMapsLocked()
			e.installedByKey[rule.key] = *ref
			e.mu.Unlock()
			appliedAdds = append(appliedAdds, rule.key)
		}
	}

	e.mu.Lock()
	e.ensureStateMapsLocked()
	next := make(map[ruleKey]ruleReference)
	for k, ref := range e.installedByKey {
		if k.policyID == policyID {
			next[k] = ref
		}
	}
	e.rebuildPolicyInstalledSetLocked(policyID, next)
	e.refreshPolicyRefsLocked(policyID)
	e.mu.Unlock()
	return nil
}

func (e *Enforcer) rebuildPolicyInstalledSetLocked(policyID string, refs map[ruleKey]ruleReference) {
	keys := make(map[ruleKey]struct{}, len(refs))
	for k := range refs {
		keys[k] = struct{}{}
	}
	if len(keys) == 0 {
		delete(e.installedByPolicy, policyID)
		return
	}
	e.installedByPolicy[policyID] = keys
}

func (e *Enforcer) refreshPolicyRefsLocked(policyID string) {
	keys := e.installedByPolicy[policyID]
	if len(keys) == 0 {
		delete(e.rules, policyID)
		return
	}
	refs := make([]ruleReference, 0, len(keys))
	for key := range keys {
		if ref, ok := e.installedByKey[key]; ok {
			refs = append(refs, ref)
		}
	}
	e.rules[policyID] = refs
}

func (e *Enforcer) ensureStateMapsLocked() {
	if e.rules == nil {
		e.rules = make(map[string][]ruleReference)
	}
	if e.specs == nil {
		e.specs = make(map[string]policy.PolicySpec)
	}
	if e.installedByPolicy == nil {
		e.installedByPolicy = make(map[string]map[ruleKey]struct{})
	}
	if e.installedByKey == nil {
		e.installedByKey = make(map[ruleKey]ruleReference)
	}
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

func (e *Enforcer) runBatch(ctx context.Context, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, e.bin, "-f", "-")
	input := strings.Join(lines, "\n") + "\n"
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("nft batch stdin: %w", err)
	}
	go func() {
		defer stdin.Close()
		_, _ = io.WriteString(stdin, input)
	}()
	output, err := cmd.CombinedOutput()
	if err != nil {
		payloadDigest := sha256.Sum256([]byte(input))
		if e.log != nil {
			e.log.Error("nft batch command failed",
				zap.String("binary", e.bin),
				zap.Int("line_count", len(lines)),
				zap.Int("payload_bytes", len(input)),
				zap.String("payload_sha256", hex.EncodeToString(payloadDigest[:])),
				zap.String("payload_preview", previewBatchPayload(input, 2048)),
				zap.ByteString("output", output),
				zap.Error(err))
		}
		return fmt.Errorf("%s -f -: %w", e.bin, err)
	}
	if e.log != nil {
		e.log.Debug("nft batch command", zap.String("binary", e.bin), zap.Int("line_count", len(lines)))
	}
	return nil
}

func previewBatchPayload(in string, max int) string {
	if max <= 0 {
		max = 1024
	}
	if len(in) <= max {
		return in
	}
	return in[:max] + "...(truncated)"
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
	var matches []uint64
	for _, entry := range dump.NFTables {
		if entry.Rule != nil && entry.Rule.Comment == comment {
			matches = append(matches, entry.Rule.Handle)
		}
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("nft: rule with comment %s not found", comment)
	}
	if len(matches) > 1 {
		return 0, fmt.Errorf("nft: rule comment %s matched multiple handles (%v)", comment, matches)
	}
	return matches[0], nil
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

func commentFor(policyID, target, chain, action, proto string, port policy.PortRange) string {
	// Encodes policy identity + strong fingerprint for lookup/restart bootstrap.
	pid := strings.TrimSpace(policyID)
	if pid == "" {
		pid = "unknown"
	}
	pidHex := hex.EncodeToString([]byte(pid))
	portSig := fmt.Sprintf("%d-%d", port.From, port.To)
	h := sha256.Sum256([]byte(target + "|" + chain + "|" + action + "|" + proto + "|" + portSig))
	return fmt.Sprintf("cmv2:pid=%s:h=%s", pidHex, hex.EncodeToString(h[:16]))
}

func policyIDFromComment(comment string) (string, bool) {
	c := strings.TrimSpace(comment)
	if !strings.HasPrefix(c, "cmv2:pid=") {
		return "", false
	}
	const marker = ":h="
	start := len("cmv2:pid=")
	idx := strings.Index(c, marker)
	if idx <= start {
		return "", false
	}
	pidHex := strings.TrimSpace(c[start:idx])
	if pidHex == "" {
		return "", false
	}
	raw, err := hex.DecodeString(pidHex)
	if err != nil || len(raw) == 0 {
		return "", false
	}
	policyID := string(raw)
	if strings.TrimSpace(policyID) == "" {
		return "", false
	}
	return policyID, true
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
