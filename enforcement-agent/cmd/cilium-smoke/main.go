package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/enforcer/cilium"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

func main() {
	var (
		timeout   = flag.Duration("timeout", 10*time.Second, "timeout for k8s calls")
		policyID  = flag.String("policy-id", "smoke-local", "policy id label (used in CRD name/labels)")
		target    = flag.String("target", "10.0.0.1", "IP/CIDR to block (dry-run by default)")
		direction = flag.String("direction", "ingress", "ingress|egress|both")
		mode      = flag.String("mode", "", "namespaced|clusterwide (overrides CILIUM_POLICY_MODE)")
		namespace = flag.String("namespace", "", "default namespace for namespaced policies (overrides ENFORCER_KUBE_NAMESPACE)")
		apply     = flag.Bool("apply", true, "run Apply() after HealthCheck()")
	)
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync() //nolint:errcheck

	cfg := cilium.Config{
		KubeConfigPath: strings.TrimSpace(os.Getenv("ENFORCER_KUBE_CONFIG")),
		Context:        strings.TrimSpace(os.Getenv("ENFORCER_KUBE_CONTEXT")),
		Namespace:      strings.TrimSpace(os.Getenv("ENFORCER_KUBE_NAMESPACE")),
		PolicyMode:     strings.TrimSpace(os.Getenv("CILIUM_POLICY_MODE")),
		LabelPrefix:    strings.TrimSpace(os.Getenv("CILIUM_LABEL_PREFIX")),
		QPS:            5,
		Burst:          10,
		DryRun:         true, // smoke is always dry-run unless the caller changes code intentionally
		Logger:         logger,
	}
	if *mode != "" {
		cfg.PolicyMode = *mode
	}
	if *namespace != "" {
		cfg.Namespace = *namespace
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "cybermesh"
	}
	if cfg.PolicyMode == "" {
		cfg.PolicyMode = "namespaced"
	}

	enf, err := cilium.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cilium-smoke: init failed: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := enf.HealthCheck(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "cilium-smoke: healthcheck failed: %v\n", err)
		os.Exit(3)
	}
	fmt.Printf("cilium-smoke: healthcheck ok (mode=%s namespace=%s dry_run=true)\n", cfg.PolicyMode, cfg.Namespace)

	if !*apply {
		return
	}

	spec := policy.PolicySpec{
		ID:       *policyID,
		RuleType: "block",
		Action:   "block",
		Target: policy.Target{
			Direction: *direction,
		},
		Guardrails: policy.Guardrails{
			DryRun: true,
		},
	}
	t := strings.TrimSpace(*target)
	if t == "" {
		fmt.Fprintf(os.Stderr, "cilium-smoke: target is required\n")
		os.Exit(4)
	}
	if strings.Contains(t, "/") {
		spec.Target.CIDRs = []string{t}
	} else {
		spec.Target.IPs = []string{t}
	}
	if strings.EqualFold(spec.Target.Direction, "both") {
		spec.Target.Direction = ""
	}

	if err := enf.Apply(ctx, spec); err != nil {
		fmt.Fprintf(os.Stderr, "cilium-smoke: apply failed: %v\n", err)
		os.Exit(5)
	}
	fmt.Printf("cilium-smoke: apply ok (dry-run)\n")
}
