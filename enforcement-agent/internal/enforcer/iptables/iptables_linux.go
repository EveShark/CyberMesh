//go:build linux

package iptables

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

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
	client kubernetes.Interface
	node   string

	mu    sync.Mutex
	rules map[string][]rule
	specs map[string]policy.PolicySpec
}

type commandRunner func(ctx context.Context, args ...string) error

type testHooks struct {
	beforeEnsure func(int, rule) error
	beforeDelete func(int, rule)
}

type iptablesCommandError struct {
	args     []string
	output   string
	exitCode int
	cause    error
}

func (e *iptablesCommandError) Error() string {
	if e == nil {
		return ""
	}
	cmd := strings.Join(e.args, " ")
	if e.output == "" {
		return fmt.Sprintf("iptables %s: %v", cmd, e.cause)
	}
	return fmt.Sprintf("iptables %s: %s: %v", cmd, e.output, e.cause)
}

func (e *iptablesCommandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
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
		node:  strings.TrimSpace(cfg.NodeName),
	}
	enf.runCmd = enf.run
	if client, err := buildKubernetesClient(cfg); err != nil {
		return nil, err
	} else {
		enf.client = client
	}
	return enf, nil
}

// Apply materialises drop/reject rules for the given policy.
func (e *Enforcer) Apply(ctx context.Context, spec policy.PolicySpec) error {
	if err := common.ValidateScope(spec); err != nil {
		return err
	}
	expanded, err := e.expandSelectorTargets(ctx, spec)
	if err != nil {
		return err
	}
	spec = expanded

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

func (e *Enforcer) expandSelectorTargets(ctx context.Context, spec policy.PolicySpec) (policy.PolicySpec, error) {
	if len(spec.Target.IPs)+len(spec.Target.CIDRs) > 0 || len(spec.Target.Selectors) == 0 {
		return spec, nil
	}

	namespace := strings.TrimSpace(spec.Target.Namespace)
	if namespace == "" {
		namespace = selectorValue(spec.Target.Selectors, "namespace", "kubernetes_namespace")
	}
	nodeName := selectorValue(spec.Target.Selectors, "node")
	if nodeName == "" {
		nodeName = strings.TrimSpace(e.node)
	}
	podSelector := selectorValue(spec.Target.Selectors, "pod_selector", "workload_selector")
	namespaceSelector := selectorValue(spec.Target.Selectors, "namespace_selector")

	needsNamespaceLookup := namespace != "" || podSelector != "" || namespaceSelector != ""
	needsNodeLookup := strings.TrimSpace(spec.Target.Selectors["node"]) != ""
	// Namespace ipset prefix only covers direct namespace selector shorthand.
	if needsNamespaceLookup && e.sets.namespacePrefix != "" && namespace != "" && podSelector == "" && namespaceSelector == "" {
		needsNamespaceLookup = false
	}
	// Node ipset prefix only covers direct node selector shorthand.
	if needsNodeLookup && e.sets.nodePrefix != "" && nodeName != "" {
		needsNodeLookup = false
	}
	if !needsNamespaceLookup && !needsNodeLookup {
		return spec, nil
	}
	if e.client == nil {
		return spec, fmt.Errorf("iptables: selector-only policy %s requires kubernetes API lookup", spec.ID)
	}
	if podSelector != "" {
		if _, err := labels.Parse(podSelector); err != nil {
			return spec, fmt.Errorf("iptables: invalid pod/workload selector %q: %w", podSelector, err)
		}
	}
	if namespaceSelector != "" {
		if _, err := labels.Parse(namespaceSelector); err != nil {
			return spec, fmt.Errorf("iptables: invalid namespace selector %q: %w", namespaceSelector, err)
		}
	}

	targetIPs := append([]string{}, spec.Target.IPs...)
	if needsNamespaceLookup {
		switch {
		case namespaceSelector != "":
			namespaces, err := e.lookupNamespacesByLabelSelector(ctx, namespaceSelector)
			if err != nil {
				return spec, err
			}
			for _, ns := range namespaces {
				ips, err := e.lookupNamespacePodIPs(ctx, ns, podSelector)
				if err != nil {
					return spec, err
				}
				targetIPs = append(targetIPs, ips...)
			}
		case namespace != "":
			ips, err := e.lookupNamespacePodIPs(ctx, namespace, podSelector)
			if err != nil {
				return spec, err
			}
			targetIPs = append(targetIPs, ips...)
		case podSelector != "":
			ips, err := e.lookupPodsByLabelSelectorAllNamespaces(ctx, podSelector)
			if err != nil {
				return spec, err
			}
			targetIPs = append(targetIPs, ips...)
		default:
			return spec, fmt.Errorf("iptables: selector-only policy %s has unresolved namespace/workload selector", spec.ID)
		}
	}
	if needsNodeLookup {
		ips, err := e.lookupNodeIPs(ctx, nodeName)
		if err != nil {
			return spec, err
		}
		targetIPs = append(targetIPs, ips...)
	}
	targetIPs = dedupeStrings(targetIPs)
	if len(targetIPs) == 0 {
		return spec, fmt.Errorf("iptables: selector-only policy %s resolved no IP targets", spec.ID)
	}
	spec.Target.IPs = targetIPs
	return spec, nil
}

func selectorValue(selectors map[string]string, keys ...string) string {
	for _, key := range keys {
		if selectors == nil {
			return ""
		}
		if value := strings.TrimSpace(selectors[key]); value != "" {
			return value
		}
	}
	return ""
}

func (e *Enforcer) lookupNamespacePodIPs(ctx context.Context, namespace string, podLabelSelector string) ([]string, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil, fmt.Errorf("iptables: namespace selector requires namespace value")
	}
	opts := metav1.ListOptions{}
	if podLabelSelector != "" {
		opts.LabelSelector = podLabelSelector
	}
	pods, err := e.client.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("iptables: list pods for namespace %s: %w", namespace, err)
	}
	ips := make([]string, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		ip := strings.TrimSpace(pod.Status.PodIP)
		if ip == "" {
			continue
		}
		ips = append(ips, ip)
	}
	return dedupeStrings(ips), nil
}

func (e *Enforcer) lookupPodsByLabelSelectorAllNamespaces(ctx context.Context, podLabelSelector string) ([]string, error) {
	pods, err := e.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{LabelSelector: podLabelSelector})
	if err != nil {
		return nil, fmt.Errorf("iptables: list pods for selector %q: %w", podLabelSelector, err)
	}
	ips := make([]string, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		ip := strings.TrimSpace(pod.Status.PodIP)
		if ip == "" {
			continue
		}
		ips = append(ips, ip)
	}
	return dedupeStrings(ips), nil
}

func (e *Enforcer) lookupNamespacesByLabelSelector(ctx context.Context, namespaceSelector string) ([]string, error) {
	namespaces, err := e.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: namespaceSelector})
	if err != nil {
		return nil, fmt.Errorf("iptables: list namespaces for selector %q: %w", namespaceSelector, err)
	}
	out := make([]string, 0, len(namespaces.Items))
	for _, ns := range namespaces.Items {
		name := strings.TrimSpace(ns.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return dedupeStrings(out), nil
}

func (e *Enforcer) lookupNodeIPs(ctx context.Context, nodeName string) ([]string, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("iptables: node selector requires node value")
	}
	node, err := e.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("iptables: get node %s: %w", nodeName, err)
	}
	ips := make([]string, 0, len(node.Status.Addresses))
	for _, addr := range node.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalIP, corev1.NodeExternalIP:
			if ip := strings.TrimSpace(addr.Address); ip != "" {
				ips = append(ips, ip)
			}
		}
	}
	return dedupeStrings(ips), nil
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func buildKubernetesClient(cfg Config) (kubernetes.Interface, error) {
	if strings.TrimSpace(cfg.KubeConfigPath) == "" {
		restCfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, nil
		}
		if cfg.QPS > 0 {
			restCfg.QPS = cfg.QPS
		}
		if cfg.Burst > 0 {
			restCfg.Burst = cfg.Burst
		}
		return kubernetes.NewForConfig(restCfg)
	}
	loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.KubeConfigPath}
	overrides := &clientcmd.ConfigOverrides{}
	if ctxName := strings.TrimSpace(cfg.Context); ctxName != "" {
		overrides.CurrentContext = ctxName
	}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, overrides)
	restCfg, err := clientCfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("iptables: load kube config: %w", err)
	}
	if cfg.QPS > 0 {
		restCfg.QPS = cfg.QPS
	}
	if cfg.Burst > 0 {
		restCfg.Burst = cfg.Burst
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("iptables: build kube client: %w", err)
	}
	return client, nil
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
	} else if !isRuleAbsentError(err) {
		// For non-absence failures (e.g. binary/runtime failures), fail closed.
		return false, err
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
		if isRuleAbsentError(err) {
			// Rule already absent; idempotent success.
			return nil
		}
		return err
	}
	return nil
}

func (e *Enforcer) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, e.bin, args...)
	// Force stable English output for deterministic error classification across locales.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))
	if err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		cmdErr := &iptablesCommandError{
			args:     append([]string(nil), args...),
			output:   outputStr,
			exitCode: exitCode,
			cause:    err,
		}

		if e.log != nil {
			if isRuleAbsentError(cmdErr) {
				e.log.Debug("iptables rule already absent", zap.String("binary", e.bin), zap.Strings("args", args), zap.String("output", outputStr))
			} else {
				e.log.Error("iptables command failed", zap.String("binary", e.bin), zap.Strings("args", args), zap.String("output", outputStr), zap.Error(err))
			}
		}
		return cmdErr
	}
	if e.log != nil {
		e.log.Debug("iptables command", zap.String("binary", e.bin), zap.Strings("args", args))
	}
	return nil
}

func isRuleAbsentError(err error) bool {
	if err == nil {
		return false
	}

	// Structured classification when command context is available.
	var cmdErr *iptablesCommandError
	if errors.As(err, &cmdErr) {
		if len(cmdErr.args) == 0 {
			return false
		}
		op := cmdErr.args[0]
		if op != "-C" && op != "-D" {
			return false
		}
		// iptables returns exit code 1 for "rule not found"/"doesn't exist" in check/delete paths.
		if cmdErr.exitCode != 1 {
			return false
		}
		out := strings.ToLower(strings.TrimSpace(cmdErr.output))
		if out == "" {
			return false
		}
		return strings.Contains(out, "bad rule (does a matching rule exist in that chain?)") ||
			strings.Contains(out, "rule does not exist") ||
			strings.Contains(out, "no chain/target/match by that name") ||
			strings.Contains(out, "rule missing")
	}

	// Fallback for tests/mocks that return plain errors.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bad rule (does a matching rule exist in that chain?)") ||
		strings.Contains(msg, "rule does not exist") ||
		strings.Contains(msg, "no chain/target/match by that name") ||
		strings.Contains(msg, "rule missing")
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
