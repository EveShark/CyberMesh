package app

import (
	"strings"
	"time"
)

var defaultAllowedActions = []string{
	"drop",
	"reject",
	"remove",
	"force_reauth",
	"disable_export",
	"freeze_user",
	"freeze_tenant",
	"throttle_action",
	"disable_ai_api_for_scope",
	"rate_limit",
}

var canonicalAllowedActions = map[string]struct{}{
	"drop":            {},
	"reject":          {},
	"remove":          {},
	"force_reauth":    {},
	"disable_export":  {},
	"freeze_user":     {},
	"freeze_tenant":   {},
	"throttle_action": {},
	"disable_ai_api_for_scope": {},
	"rate_limit":      {},
}

func IsCanonicalAllowedAction(action string) bool {
	_, ok := canonicalAllowedActions[strings.ToLower(strings.TrimSpace(action))]
	return ok
}

type Config struct {
	BaseURL        string
	CommandPath    string
	HealthPath     string
	Timeout        time.Duration
	BearerToken    string
	AllowedActions []string
}

func (c Config) withDefaults() Config {
	out := c
	out.BaseURL = strings.TrimRight(strings.TrimSpace(out.BaseURL), "/")
	if strings.TrimSpace(out.CommandPath) == "" {
		out.CommandPath = "/v1/enforcement/commands"
	}
	if strings.TrimSpace(out.HealthPath) == "" {
		out.HealthPath = "/healthz"
	}
	if out.Timeout <= 0 {
		out.Timeout = 2 * time.Second
	}
	if len(out.AllowedActions) == 0 {
		out.AllowedActions = append([]string(nil), defaultAllowedActions...)
	}
	return out
}
