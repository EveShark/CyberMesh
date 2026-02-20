package cilium

import "go.uber.org/zap"

// Config captures options for the Cilium enforcement backend.
//
// The backend is a Kubernetes client that applies Cilium policy CRDs:
// - CiliumNetworkPolicy (namespaced)
// - CiliumClusterwideNetworkPolicy (clusterwide)
type Config struct {
	KubeConfigPath string
	Context        string

	// Namespace is the default namespace used for namespaced policies when
	// the PolicySpec doesn't provide one (target.namespace or selectors.namespace).
	Namespace string

	// PolicyMode controls which CRD to use.
	// Supported values: "namespaced", "clusterwide".
	PolicyMode string

	// PolicyNamespace overrides where namespaced policies are created. When empty,
	// the backend uses the resolved effective namespace (PolicySpec or Namespace).
	PolicyNamespace string

	// LabelPrefix is applied to selector keys that do not already look qualified.
	// Default: "k8s:".
	LabelPrefix string

	QPS    float32
	Burst  int
	DryRun bool
	Logger *zap.Logger
}
