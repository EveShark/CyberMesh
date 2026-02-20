package cilium

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/CyberMesh/enforcement-agent/internal/enforcer/common"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

const (
	ciliumGroup   = "cilium.io"
	ciliumVersion = "v2"

	modeNamespaced  = "namespaced"
	modeClusterwide = "clusterwide"

	defaultLabelPrefix = "k8s:"
)

var (
	gvrCNP  = schema.GroupVersionResource{Group: ciliumGroup, Version: ciliumVersion, Resource: "ciliumnetworkpolicies"}
	gvrCCNP = schema.GroupVersionResource{Group: ciliumGroup, Version: ciliumVersion, Resource: "ciliumclusterwidenetworkpolicies"}
)

// Enforcer applies Cilium policy CRDs.
type Enforcer struct {
	client dynamic.Interface

	mode            string
	defaultNS       string
	policyNamespace string
	labelPrefix     string
	dryRun          bool
	log             *zap.Logger

	mu    sync.Mutex
	specs map[string]policy.PolicySpec
	meta  map[string]resourceLocator
}

type resourceLocator struct {
	Namespace string
	Name      string
	GVR       schema.GroupVersionResource
}

// New constructs the Cilium backend.
func New(cfg Config) (*Enforcer, error) {
	restCfg, err := loadConfig(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.QPS > 0 {
		restCfg.QPS = cfg.QPS
	}
	if cfg.Burst > 0 {
		restCfg.Burst = cfg.Burst
	}

	cli, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("cilium: build client: %w", err)
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.PolicyMode))
	if mode == "" {
		mode = modeNamespaced
	}
	if mode != modeNamespaced && mode != modeClusterwide {
		return nil, fmt.Errorf("cilium: invalid policy mode %s", cfg.PolicyMode)
	}

	ns := strings.TrimSpace(cfg.Namespace)
	if ns == "" {
		ns = "default"
	}

	labelPrefix := strings.TrimSpace(cfg.LabelPrefix)
	if labelPrefix == "" {
		labelPrefix = defaultLabelPrefix
	}

	return &Enforcer{
		client:          cli,
		mode:            mode,
		defaultNS:       ns,
		policyNamespace: strings.TrimSpace(cfg.PolicyNamespace),
		labelPrefix:     labelPrefix,
		dryRun:          cfg.DryRun,
		log:             cfg.Logger,
		specs:           make(map[string]policy.PolicySpec),
		meta:            make(map[string]resourceLocator),
	}, nil
}

// HealthCheck verifies that the Cilium policy CRD is reachable with current RBAC.
func (e *Enforcer) HealthCheck(ctx context.Context) error {
	if e == nil || e.client == nil {
		return errors.New("cilium: client not initialized")
	}
	gvr := e.gvr()
	if gvr == gvrCCNP {
		_, err := e.client.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			return fmt.Errorf("cilium: list %s: %w", gvr.Resource, err)
		}
		return nil
	}
	ns := e.defaultNS
	_, err := e.client.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("cilium: list %s in %s: %w", gvr.Resource, ns, err)
	}
	return nil
}

// ReadyCheck delegates to HealthCheck.
func (e *Enforcer) ReadyCheck(ctx context.Context) error { return e.HealthCheck(ctx) }

// Apply creates or updates a Cilium policy implementing the block.
func (e *Enforcer) Apply(ctx context.Context, spec policy.PolicySpec) error {
	if err := common.ValidateScope(spec); err != nil {
		return err
	}

	// Match iptables/nftables semantics: a "block" policy must specify targets.
	if len(spec.Target.IPs) == 0 && len(spec.Target.CIDRs) == 0 {
		return fmt.Errorf("cilium: policy %s missing target IPs/CIDRs", spec.ID)
	}

	name := sanitizeName("cm-block-" + spec.ID)
	if name == "" {
		return fmt.Errorf("cilium: unable to derive name for policy %s", spec.ID)
	}

	obj, gvr, ns, err := buildPolicyObject(spec, name, e.mode, e.effectiveNamespace(spec), e.labelPrefix)
	if err != nil {
		return err
	}
	if e.policyNamespace != "" && gvr == gvrCNP {
		ns = e.policyNamespace
		obj.SetNamespace(ns)
	}

	dryRun := e.dryRun || spec.Guardrails.DryRun
	createOpts := metav1.CreateOptions{}
	updateOpts := metav1.UpdateOptions{}
	if dryRun {
		createOpts.DryRun = []string{metav1.DryRunAll}
		updateOpts.DryRun = []string{metav1.DryRunAll}
	}

	var res dynamic.ResourceInterface
	if gvr == gvrCCNP {
		res = e.client.Resource(gvr)
	} else {
		res = e.client.Resource(gvr).Namespace(ns)
	}

	_, err = res.Create(ctx, obj, createOpts)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing, getErr := res.Get(ctx, name, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("cilium: update fetch %s: %w", name, getErr)
			}
			// Preserve resourceVersion for Update.
			obj.SetResourceVersion(existing.GetResourceVersion())
			if _, err = res.Update(ctx, obj, updateOpts); err != nil {
				return fmt.Errorf("cilium: update %s: %w", name, err)
			}
		} else {
			return fmt.Errorf("cilium: create %s: %w", name, err)
		}
	}

	e.mu.Lock()
	e.specs[spec.ID] = spec
	e.meta[spec.ID] = resourceLocator{Namespace: ns, Name: name, GVR: gvr}
	e.mu.Unlock()

	if e.log != nil {
		e.log.Info("applied cilium policy",
			zap.String("policy_id", spec.ID),
			zap.String("name", name),
			zap.String("mode", e.mode),
			zap.String("namespace", ns),
			zap.Bool("dry_run", dryRun),
			zap.Time("ts", time.Now().UTC()))
	}

	return nil
}

// Remove deletes the Cilium policy associated with the policy id.
func (e *Enforcer) Remove(ctx context.Context, policyID string) error {
	e.mu.Lock()
	locator, ok := e.meta[policyID]
	spec, specOK := e.specs[policyID]
	e.mu.Unlock()
	if !ok {
		return nil
	}

	dryRun := e.dryRun
	if specOK {
		dryRun = dryRun || spec.Guardrails.DryRun
	}
	options := metav1.DeleteOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}

	var res dynamic.ResourceInterface
	if locator.GVR == gvrCCNP {
		res = e.client.Resource(locator.GVR)
	} else {
		res = e.client.Resource(locator.GVR).Namespace(locator.Namespace)
	}

	if err := res.Delete(ctx, locator.Name, options); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("cilium: delete %s: %w", locator.Name, err)
	}

	e.mu.Lock()
	delete(e.meta, policyID)
	delete(e.specs, policyID)
	e.mu.Unlock()

	if e.log != nil {
		e.log.Info("removed cilium policy",
			zap.String("policy_id", policyID),
			zap.String("name", locator.Name),
			zap.String("namespace", locator.Namespace),
			zap.Bool("dry_run", dryRun),
			zap.Time("ts", time.Now().UTC()))
	}
	return nil
}

// List returns tracked policies.
func (e *Enforcer) List(context.Context) ([]policy.PolicySpec, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	res := make([]policy.PolicySpec, 0, len(e.specs))
	for _, spec := range e.specs {
		res = append(res, spec)
	}
	return res, nil
}

func (e *Enforcer) gvr() schema.GroupVersionResource {
	if e.mode == modeClusterwide {
		return gvrCCNP
	}
	return gvrCNP
}

func (e *Enforcer) effectiveNamespace(spec policy.PolicySpec) string {
	if e.mode == modeClusterwide {
		return ""
	}
	if ns := strings.TrimSpace(spec.Target.Namespace); ns != "" {
		return ns
	}
	if ns, ok := spec.Target.Selectors["namespace"]; ok {
		ns = strings.ToLower(strings.TrimSpace(ns))
		if ns != "" {
			return ns
		}
	}
	return e.defaultNS
}

func loadConfig(cfg Config) (*rest.Config, error) {
	if strings.TrimSpace(cfg.KubeConfigPath) != "" {
		loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.KubeConfigPath}
		overrides := &clientcmd.ConfigOverrides{}
		if strings.TrimSpace(cfg.Context) != "" {
			overrides.CurrentContext = cfg.Context
		}
		clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, overrides)
		restCfg, err := clientCfg.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("cilium: load kubeconfig: %w", err)
		}
		return restCfg, nil
	}
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("cilium: in-cluster config not available")
		}
		return nil, fmt.Errorf("cilium: load in-cluster config: %w", err)
	}
	return restCfg, nil
}

// buildPolicyObject constructs an unstructured Cilium policy CRD.
func buildPolicyObject(spec policy.PolicySpec, name, mode, namespace, labelPrefix string) (*unstructured.Unstructured, schema.GroupVersionResource, string, error) {
	if labelPrefix == "" {
		labelPrefix = defaultLabelPrefix
	}

	labels := map[string]any{
		"managed-by": "cybermesh",
		"policy-id":  spec.ID,
	}
	if spec.Tenant != "" {
		labels["tenant"] = spec.Tenant
	}
	if spec.Region != "" {
		labels["region"] = spec.Region
	}

	// Optional: include rule hash for traceability when the producer provides it.
	if raw, ok := spec.Raw["rule_hash"].([]byte); ok && len(raw) > 0 {
		labels["rule-hash"] = hex.EncodeToString(raw)
	}

	endpointSelector := buildEndpointSelector(spec.Target.Selectors, spec.Target.Namespace, labelPrefix)
	targetCIDRs, err := buildTargetCIDRs(spec.Target.IPs, spec.Target.CIDRs)
	if err != nil {
		return nil, schema.GroupVersionResource{}, "", err
	}

	toPorts, err := buildToPorts(spec.Target.Ports, spec.Target.Protocols)
	if err != nil {
		return nil, schema.GroupVersionResource{}, "", err
	}

	var ingressDeny []any
	var egressDeny []any
	dir := strings.ToLower(strings.TrimSpace(spec.Target.Direction))
	if dir == "" || dir == "ingress" {
		rule := map[string]any{
			"fromCIDR": targetCIDRs,
		}
		if len(toPorts) > 0 {
			rule["toPorts"] = toPorts
		}
		ingressDeny = append(ingressDeny, rule)
	}
	if dir == "" || dir == "egress" {
		rule := map[string]any{
			"toCIDR": targetCIDRs,
		}
		if len(toPorts) > 0 {
			rule["toPorts"] = toPorts
		}
		egressDeny = append(egressDeny, rule)
	}

	apiVersion := fmt.Sprintf("%s/%s", ciliumGroup, ciliumVersion)

	kind := "CiliumNetworkPolicy"
	gvr := gvrCNP
	ns := namespace
	if strings.ToLower(strings.TrimSpace(mode)) == modeClusterwide {
		kind = "CiliumClusterwideNetworkPolicy"
		gvr = gvrCCNP
		ns = ""
	}

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]any{
				"name":   name,
				"labels": labels,
			},
			"spec": map[string]any{
				"endpointSelector": endpointSelector,
			},
		},
	}
	if ns != "" && gvr == gvrCNP {
		obj.Object["metadata"].(map[string]any)["namespace"] = ns
	}
	specObj := obj.Object["spec"].(map[string]any)
	if len(ingressDeny) > 0 {
		specObj["ingressDeny"] = ingressDeny
	}
	if len(egressDeny) > 0 {
		specObj["egressDeny"] = egressDeny
	}

	return obj, gvr, ns, nil
}

func buildEndpointSelector(sel map[string]string, ns string, labelPrefix string) map[string]any {
	match := make(map[string]any, len(sel)+1)

	// Namespace is represented as a special Cilium label.
	if strings.TrimSpace(ns) == "" {
		if raw, ok := sel["namespace"]; ok {
			ns = strings.TrimSpace(raw)
		}
	}
	if strings.TrimSpace(ns) != "" {
		match["k8s:io.kubernetes.pod.namespace"] = strings.ToLower(strings.TrimSpace(ns))
	}

	for k, v := range sel {
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		if strings.EqualFold(key, "namespace") {
			continue
		}
		if !strings.Contains(key, ":") && labelPrefix != "" {
			key = labelPrefix + key
		}
		match[key] = val
	}

	return map[string]any{"matchLabels": match}
}

func buildTargetCIDRs(ips []string, cidrs []string) ([]any, error) {
	out := make([]any, 0, len(ips)+len(cidrs))
	for _, raw := range ips {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		// Convert IP to a single-host CIDR.
		if strings.Contains(ip, ":") {
			out = append(out, ip+"/128")
		} else {
			out = append(out, ip+"/32")
		}
	}
	for _, raw := range cidrs {
		c := strings.TrimSpace(raw)
		if c == "" {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("cilium: no valid target CIDRs/IPs")
	}
	return out, nil
}

func buildToPorts(ranges []policy.PortRange, protocols []string) ([]any, error) {
	if len(ranges) == 0 {
		return nil, nil
	}
	if len(protocols) == 0 {
		return nil, errors.New("cilium: port range specified without protocol")
	}
	normalized := make([]string, 0, len(protocols))
	for _, p := range protocols {
		proto := strings.ToUpper(strings.TrimSpace(p))
		switch proto {
		case "TCP", "UDP":
			normalized = append(normalized, proto)
		default:
			return nil, fmt.Errorf("cilium: ports specified but protocol %s does not support ports", proto)
		}
	}
	if len(normalized) == 0 {
		return nil, errors.New("cilium: no transport protocols for ports")
	}

	var ports []any
	for _, pr := range ranges {
		from := pr.From
		to := pr.To
		if from <= 0 || from > 65535 {
			return nil, fmt.Errorf("cilium: invalid port %d", from)
		}
		if to == 0 {
			to = from
		}
		if to < from || to > 65535 {
			return nil, fmt.Errorf("cilium: invalid port range %d-%d", from, to)
		}

		for _, proto := range normalized {
			ports = append(ports, map[string]any{
				"port":     fmt.Sprintf("%d", from),
				"protocol": proto,
			})
			// Cilium supports port ranges via "port" + "endPort" on recent versions, but
			// to keep CRD compatibility conservative we expand ranges.
			for p := from + 1; p <= to; p++ {
				ports = append(ports, map[string]any{
					"port":     fmt.Sprintf("%d", p),
					"protocol": proto,
				})
			}
		}
	}

	if len(ports) == 0 {
		return nil, nil
	}
	return []any{map[string]any{"ports": ports}}, nil
}

var dns1123 = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = dns1123.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = s[:63]
		s = strings.Trim(s, "-")
	}
	return s
}
