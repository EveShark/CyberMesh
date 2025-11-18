package kubernetes

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/CyberMesh/enforcement-agent/internal/enforcer/common"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Config captures options for the Kubernetes enforcement backend.
type Config struct {
	KubeConfigPath string
	Context        string
	Namespace      string
	QPS            float32
	Burst          int
	DryRun         bool
	Logger         *zap.Logger
}

// Enforcer creates NetworkPolicy resources that deny traffic to/from blocked targets.
type Enforcer struct {
	client    kubernetes.Interface
	namespace string
	dryRun    bool
	log       *zap.Logger

	mu    sync.Mutex
	specs map[string]policy.PolicySpec
	meta  map[string]resourceLocator
}

type resourceLocator struct {
	Namespace string
	Name      string
}

// New constructs the Kubernetes backend.
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

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: build client: %w", err)
	}

	ns := strings.TrimSpace(cfg.Namespace)
	if ns == "" {
		ns = "default"
	}

	return &Enforcer{
		client:    clientset,
		namespace: ns,
		dryRun:    cfg.DryRun,
		log:       cfg.Logger,
		specs:     make(map[string]policy.PolicySpec),
		meta:      make(map[string]resourceLocator),
	}, nil
}

// Apply renders/updates a NetworkPolicy implementing the block.
func (e *Enforcer) Apply(ctx context.Context, spec policy.PolicySpec) error {
	if err := common.ValidateScope(spec); err != nil {
		return err
	}

	policyName := sanitizeName("cm-block-" + spec.ID)
	if policyName == "" {
		return fmt.Errorf("kubernetes: unable to derive name for policy %s", spec.ID)
	}

	ipv4Except, ipv6Except, err := buildIPBlockExcept(spec)
	if err != nil {
		return err
	}

	ports, err := buildPolicyPorts(spec.Target.Ports, spec.Target.Protocols)
	if err != nil {
		return err
	}

	ns := e.effectiveNamespace(spec)
	selector := buildPodSelector(spec.Target.Selectors)
	policyTypes := determinePolicyTypes(spec.Target.Direction)
	dryRun := e.dryRun || spec.Guardrails.DryRun

	policyObj := &v1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: ns,
			Labels: map[string]string{
				"managed-by": "cybermesh",
				"policy-id":  spec.ID,
			},
		},
		Spec: v1.NetworkPolicySpec{
			PodSelector: selector,
		},
	}

	if len(policyTypes) == 0 {
		policyTypes = []v1.PolicyType{v1.PolicyTypeIngress, v1.PolicyTypeEgress}
	}
	policyObj.Spec.PolicyTypes = policyTypes

	if needIngress(policyTypes) {
		ingressRule := v1.NetworkPolicyIngressRule{Ports: ports}
		if len(ipv4Except) > 0 {
			ingressRule.From = append(ingressRule.From, v1.NetworkPolicyPeer{IPBlock: &v1.IPBlock{CIDR: "0.0.0.0/0", Except: ipv4Except}})
		}
		if len(ipv6Except) > 0 {
			ingressRule.From = append(ingressRule.From, v1.NetworkPolicyPeer{IPBlock: &v1.IPBlock{CIDR: "::/0", Except: ipv6Except}})
		}
		if len(ingressRule.From) == 0 && len(ports) == 0 {
			policyObj.Spec.Ingress = []v1.NetworkPolicyIngressRule{{}}
		} else {
			policyObj.Spec.Ingress = []v1.NetworkPolicyIngressRule{ingressRule}
		}
	}

	if needEgress(policyTypes) {
		egressRule := v1.NetworkPolicyEgressRule{Ports: ports}
		if len(ipv4Except) > 0 {
			egressRule.To = append(egressRule.To, v1.NetworkPolicyPeer{IPBlock: &v1.IPBlock{CIDR: "0.0.0.0/0", Except: ipv4Except}})
		}
		if len(ipv6Except) > 0 {
			egressRule.To = append(egressRule.To, v1.NetworkPolicyPeer{IPBlock: &v1.IPBlock{CIDR: "::/0", Except: ipv6Except}})
		}
		if len(egressRule.To) == 0 && len(ports) == 0 {
			policyObj.Spec.Egress = []v1.NetworkPolicyEgressRule{{}}
		} else {
			policyObj.Spec.Egress = []v1.NetworkPolicyEgressRule{egressRule}
		}
	}

	options := metav1.CreateOptions{}
	updateOptions := metav1.UpdateOptions{}
	if dryRun {
		dryRunFlag := []string{metav1.DryRunAll}
		options.DryRun = dryRunFlag
		updateOptions.DryRun = dryRunFlag
	}

	client := e.client.NetworkingV1().NetworkPolicies(ns)
	_, err = client.Create(ctx, policyObj, options)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing, getErr := client.Get(ctx, policyObj.Name, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("kubernetes: update fetch %s/%s: %w", ns, policyObj.Name, getErr)
			}
			existing.Spec = policyObj.Spec
			existing.Labels = policyObj.Labels
			if _, err = client.Update(ctx, existing, updateOptions); err != nil {
				return fmt.Errorf("kubernetes: update %s/%s: %w", ns, policyObj.Name, err)
			}
		} else {
			return fmt.Errorf("kubernetes: create %s/%s: %w", ns, policyObj.Name, err)
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.specs[spec.ID] = spec
	e.meta[spec.ID] = resourceLocator{Namespace: ns, Name: policyObj.Name}

	if e.log != nil {
		e.log.Info("applied kubernetes networkpolicy",
			zap.String("policy_id", spec.ID),
			zap.String("namespace", ns),
			zap.String("name", policyObj.Name),
			zap.Bool("dry_run", dryRun),
			zap.Time("ts", time.Now().UTC()),
		)
	}

	return nil
}

// Remove deletes the NetworkPolicy associated with the policy.
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
		dryRunFlag := []string{metav1.DryRunAll}
		options.DryRun = dryRunFlag
	}

	client := e.client.NetworkingV1().NetworkPolicies(locator.Namespace)
	if err := client.Delete(ctx, locator.Name, options); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("kubernetes: delete %s/%s: %w", locator.Namespace, locator.Name, err)
	}

	e.mu.Lock()
	delete(e.meta, policyID)
	delete(e.specs, policyID)
	e.mu.Unlock()

	if e.log != nil {
		e.log.Info("removed kubernetes networkpolicy",
			zap.String("policy_id", policyID),
			zap.String("namespace", locator.Namespace),
			zap.String("name", locator.Name),
			zap.Bool("dry_run", dryRun),
			zap.Time("ts", time.Now().UTC()),
		)
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

// HealthCheck confirms Kubernetes API reachability.
func (e *Enforcer) HealthCheck(ctx context.Context) error {
	_, err := e.client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("kubernetes: server version: %w", err)
	}
	return nil
}

// ReadyCheck reuses HealthCheck for readiness signal.
func (e *Enforcer) ReadyCheck(ctx context.Context) error {
	return e.HealthCheck(ctx)
}

func loadConfig(cfg Config) (*rest.Config, error) {
	if cfg.KubeConfigPath != "" || cfg.Context != "" {
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.KubeConfigPath}
		overrides := &clientcmd.ConfigOverrides{}
		if cfg.Context != "" {
			overrides.CurrentContext = cfg.Context
		}
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
		restCfg, err := clientConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("kubernetes: load kubeconfig: %w", err)
		}
		return restCfg, nil
	}

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kubernetes: load in-cluster config: %w", err)
	}
	return restCfg, nil
}

func buildPodSelector(selectors map[string]string) metav1.LabelSelector {
	labels := make(map[string]string)
	for k, v := range selectors {
		if strings.EqualFold(k, "namespace") {
			continue
		}
		labels[k] = v
	}
	return metav1.LabelSelector{MatchLabels: labels}
}

func determinePolicyTypes(direction string) []v1.PolicyType {
	dir := strings.ToLower(strings.TrimSpace(direction))
	switch dir {
	case "", "any", "both":
		return []v1.PolicyType{v1.PolicyTypeIngress, v1.PolicyTypeEgress}
	case "ingress", "inbound":
		return []v1.PolicyType{v1.PolicyTypeIngress}
	case "egress", "outbound":
		return []v1.PolicyType{v1.PolicyTypeEgress}
	default:
		return []v1.PolicyType{}
	}
}

func needIngress(types []v1.PolicyType) bool {
	for _, t := range types {
		if t == v1.PolicyTypeIngress {
			return true
		}
	}
	return false
}

func needEgress(types []v1.PolicyType) bool {
	for _, t := range types {
		if t == v1.PolicyTypeEgress {
			return true
		}
	}
	return false
}

func buildPolicyPorts(ranges []policy.PortRange, protocols []string) ([]v1.NetworkPolicyPort, error) {
	if len(ranges) == 0 && len(protocols) == 0 {
		return nil, nil
	}

	normalized := normalizePolicyProtocols(protocols, len(ranges) > 0)
	ports := make([]v1.NetworkPolicyPort, 0)

	if len(ranges) == 0 {
		for _, proto := range normalized {
			if proto == "" || proto == "any" {
				continue
			}
			ptr, err := protocolPointer(proto)
			if err != nil {
				return nil, err
			}
			ports = append(ports, v1.NetworkPolicyPort{Protocol: ptr})
		}
		if len(ports) == 0 {
			return nil, nil
		}
		return ports, nil
	}

	for _, proto := range normalized {
		if proto == "" || proto == "any" {
			return nil, fmt.Errorf("kubernetes: port range specified without concrete protocol")
		}
		ptr, err := protocolPointer(proto)
		if err != nil {
			return nil, err
		}
		for _, pr := range ranges {
			if pr.From <= 0 || pr.From > 65535 {
				return nil, fmt.Errorf("kubernetes: invalid port %d", pr.From)
			}
			if pr.To != 0 && (pr.To > 65535 || pr.To < pr.From) {
				return nil, fmt.Errorf("kubernetes: invalid port range %d-%d", pr.From, pr.To)
			}
			npPort := v1.NetworkPolicyPort{Protocol: ptr}
			port := intstr.FromInt(pr.From)
			npPort.Port = &port
			if pr.To > pr.From {
				end := int32(pr.To)
				npPort.EndPort = &end
			}
			ports = append(ports, npPort)
		}
	}

	return ports, nil
}

func normalizePolicyProtocols(protocols []string, portsDefined bool) []string {
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

func protocolPointer(protocol string) (*corev1.Protocol, error) {
	switch strings.ToUpper(protocol) {
	case "TCP":
		p := corev1.ProtocolTCP
		return &p, nil
	case "UDP":
		p := corev1.ProtocolUDP
		return &p, nil
	case "SCTP":
		p := corev1.ProtocolSCTP
		return &p, nil
	case "UDPLITE":
		proto := corev1.Protocol("UDPLite")
		return &proto, nil
	default:
		return nil, fmt.Errorf("kubernetes: unsupported protocol %s", protocol)
	}
}

func buildIPBlockExcept(spec policy.PolicySpec) ([]string, []string, error) {
	var ipv4Except []string
	var ipv6Except []string

	targets := append([]string{}, spec.Target.IPs...)
	targets = append(targets, spec.Target.CIDRs...)
	var errs []error
	for _, target := range targets {
		cidr, ipv6, err := normaliseTarget(target)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ipv6 {
			ipv6Except = append(ipv6Except, cidr)
		} else {
			ipv4Except = append(ipv4Except, cidr)
		}
	}
	if len(errs) > 0 {
		return nil, nil, utilerrors.NewAggregate(errs)
	}
	return ipv4Except, ipv6Except, nil
}

func normaliseTarget(target string) (string, bool, error) {
	t := strings.TrimSpace(target)
	if t == "" {
		return "", false, fmt.Errorf("empty target")
	}
	if strings.Contains(t, "/") {
		_, network, err := net.ParseCIDR(t)
		if err != nil {
			return "", false, fmt.Errorf("invalid cidr %s", t)
		}
		if network.IP.To4() != nil {
			return network.String(), false, nil
		}
		return network.String(), true, nil
	}
	ip := net.ParseIP(t)
	if ip == nil {
		return "", false, fmt.Errorf("invalid ip %s", t)
	}
	if ip.To4() != nil {
		return fmt.Sprintf("%s/32", ip.String()), false, nil
	}
	return fmt.Sprintf("%s/128", ip.String()), true, nil
}

var dns1123 = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeName(name string) string {
	lower := strings.ToLower(name)
	lower = dns1123.ReplaceAllString(lower, "-")
	lower = strings.Trim(lower, "-")
	if len(lower) > 63 {
		lower = lower[:63]
	}
	if lower == "" {
		return ""
	}
	if lower[0] == '-' {
		lower = "cm" + lower
	}
	return lower
}

func (e *Enforcer) effectiveNamespace(spec policy.PolicySpec) string {
	ns := e.namespace
	if spec.Target.Namespace != "" {
		ns = spec.Target.Namespace
	} else if selectorNS, ok := spec.Target.Selectors["namespace"]; ok {
		trim := strings.TrimSpace(selectorNS)
		if trim != "" {
			ns = trim
		}
	}
	return ns
}
