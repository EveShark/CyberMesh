package nftables

import "go.uber.org/zap"

// Config captures options for the nftables backend.
type Config struct {
	Binary             string
	DryRun             bool
	Logger             *zap.Logger
	NamespaceSetPrefix string
	NodeSetPrefix      string
}
