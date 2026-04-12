package config

import (
	"testing"
	"time"

	"backend/pkg/utils"
)

type staticConfigSource map[string]string

func (s staticConfigSource) Get(key string) (string, bool) {
	value, ok := s[key]
	return value, ok
}

func (s staticConfigSource) Set(key, value string) error {
	s[key] = value
	return nil
}

func (s staticConfigSource) Delete(key string) error {
	delete(s, key)
	return nil
}

func (s staticConfigSource) List() map[string]string {
	out := make(map[string]string, len(s))
	for key, value := range s {
		out[key] = value
	}
	return out
}

func TestLoadAPIConfigDefaultsOpenFGAFlagsFromConnectionSettings(t *testing.T) {
	t.Parallel()

	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Source: staticConfigSource{
			"ENVIRONMENT":     "development",
			"API_TLS_ENABLED": "false",
			"FGA_API_URL":     "http://localhost:8080",
			"FGA_STORE_ID":    "store-1",
			"FGA_MODEL_ID":    "model-1",
		},
	})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}

	cfg, err := LoadAPIConfig(cm)
	if err != nil {
		t.Fatalf("LoadAPIConfig: %v", err)
	}
	if !cfg.OpenFGAShadow {
		t.Fatal("expected shadow enabled by default when URL/store/model are configured")
	}
	if cfg.OpenFGAShadowMaxInflight != 64 {
		t.Fatalf("shadow_max_inflight=%d", cfg.OpenFGAShadowMaxInflight)
	}
	if cfg.BreakGlassEnabled {
		t.Fatal("expected break-glass disabled by default")
	}
	if cfg.BreakGlassMaxDuration != time.Hour {
		t.Fatalf("break_glass_max_duration=%s", cfg.BreakGlassMaxDuration)
	}
	if cfg.ZitadelJWKSRefreshInterval != time.Hour {
		t.Fatalf("jwks_refresh_interval=%s", cfg.ZitadelJWKSRefreshInterval)
	}
	if cfg.ZitadelJWKSTimeout != 5*time.Second {
		t.Fatalf("jwks_timeout=%s", cfg.ZitadelJWKSTimeout)
	}
	if cfg.OpenFGAEnforce {
		t.Fatal("expected enforce disabled by default")
	}
	if !cfg.OpenFGATupleReconcileEnabled {
		t.Fatal("expected tuple reconcile enabled by default when URL/store/model are configured")
	}
	if cfg.OpenFGATimeout != 750*time.Millisecond {
		t.Fatalf("timeout=%s", cfg.OpenFGATimeout)
	}
}

func TestLoadAPIConfigParsesExplicitOpenFGAOverrides(t *testing.T) {
	t.Parallel()

	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Source: staticConfigSource{
			"ENVIRONMENT":                   "development",
			"API_TLS_ENABLED":               "false",
			"ZITADEL_JWKS_REFRESH_INTERVAL": "30m",
			"ZITADEL_JWKS_TIMEOUT":          "2s",
			"FGA_API_URL":                   "http://localhost:8080",
			"FGA_STORE_ID":                  "store-1",
			"FGA_MODEL_ID":                  "model-1",
			"FGA_SHADOW_ENABLED":            "false",
			"FGA_ENFORCE_ENABLED":           "true",
			"FGA_ENFORCE_RESOURCE_TYPES":    "policy, workflow ,audit_scope",
			"FGA_TIMEOUT":                   "1200ms",
			"FGA_SHADOW_MAX_INFLIGHT":       "25",
			"FGA_TUPLE_RECONCILE_ENABLED":   "false",
			"FGA_TUPLE_RECONCILE_INTERVAL":  "75s",
			"FGA_TUPLE_BATCH_SIZE":          "150",
			"BREAK_GLASS_ENABLED":           "true",
			"BREAK_GLASS_MAX_DURATION":      "30m",
		},
	})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}

	cfg, err := LoadAPIConfig(cm)
	if err != nil {
		t.Fatalf("LoadAPIConfig: %v", err)
	}
	if cfg.OpenFGAShadow {
		t.Fatal("expected explicit shadow disable to win")
	}
	if cfg.ZitadelJWKSRefreshInterval != 30*time.Minute {
		t.Fatalf("jwks_refresh_interval=%s", cfg.ZitadelJWKSRefreshInterval)
	}
	if cfg.ZitadelJWKSTimeout != 2*time.Second {
		t.Fatalf("jwks_timeout=%s", cfg.ZitadelJWKSTimeout)
	}
	if !cfg.OpenFGAEnforce {
		t.Fatal("expected explicit enforce enable to win")
	}
	if cfg.OpenFGATimeout != 1200*time.Millisecond {
		t.Fatalf("timeout=%s", cfg.OpenFGATimeout)
	}
	if cfg.OpenFGAShadowMaxInflight != 25 {
		t.Fatalf("shadow_max_inflight=%d", cfg.OpenFGAShadowMaxInflight)
	}
	if !cfg.BreakGlassEnabled {
		t.Fatal("expected break-glass enabled")
	}
	if cfg.BreakGlassMaxDuration != 30*time.Minute {
		t.Fatalf("break_glass_max_duration=%s", cfg.BreakGlassMaxDuration)
	}
	if cfg.OpenFGATupleReconcileEnabled {
		t.Fatal("expected explicit tuple reconcile disable to win")
	}
	if cfg.OpenFGATupleReconcileInterval != 75*time.Second {
		t.Fatalf("interval=%s", cfg.OpenFGATupleReconcileInterval)
	}
	if cfg.OpenFGATupleBatchSize != 150 {
		t.Fatalf("batch_size=%d", cfg.OpenFGATupleBatchSize)
	}
	if len(cfg.OpenFGAEnforceTypes) != 3 || cfg.OpenFGAEnforceTypes[0] != "policy" || cfg.OpenFGAEnforceTypes[1] != "workflow" || cfg.OpenFGAEnforceTypes[2] != "audit_scope" {
		t.Fatalf("types=%#v", cfg.OpenFGAEnforceTypes)
	}
}

func TestLoadAPIConfigRejectsInvalidOpenFGAEnforceResourceType(t *testing.T) {
	t.Parallel()

	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Source: staticConfigSource{
			"ENVIRONMENT":                "development",
			"API_TLS_ENABLED":            "false",
			"FGA_ENFORCE_RESOURCE_TYPES": "policy,unknown_type",
		},
	})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}

	_, err = LoadAPIConfig(cm)
	if err == nil {
		t.Fatal("expected invalid enforce type error")
	}
}

func TestLoadAPIConfigNormalizesAndDeduplicatesOpenFGAEnforceTypes(t *testing.T) {
	t.Parallel()

	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Source: staticConfigSource{
			"ENVIRONMENT":                "development",
			"API_TLS_ENABLED":            "false",
			"FGA_ENFORCE_RESOURCE_TYPES": "Policy, workflow, platform_config, policy",
		},
	})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}

	cfg, err := LoadAPIConfig(cm)
	if err != nil {
		t.Fatalf("LoadAPIConfig: %v", err)
	}
	if len(cfg.OpenFGAEnforceTypes) != 3 {
		t.Fatalf("types=%#v", cfg.OpenFGAEnforceTypes)
	}
	if cfg.OpenFGAEnforceTypes[0] != "policy" || cfg.OpenFGAEnforceTypes[1] != "workflow" || cfg.OpenFGAEnforceTypes[2] != "platform_config" {
		t.Fatalf("types=%#v", cfg.OpenFGAEnforceTypes)
	}
}

func TestLoadAPIConfigRejectsInvalidOpenFGAShadowMaxInflight(t *testing.T) {
	t.Parallel()

	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Source: staticConfigSource{
			"ENVIRONMENT":             "development",
			"API_TLS_ENABLED":         "false",
			"FGA_SHADOW_MAX_INFLIGHT": "-1",
		},
	})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}

	_, err = LoadAPIConfig(cm)
	if err == nil {
		t.Fatal("expected invalid shadow max inflight error")
	}
}

func TestLoadAPIConfigRejectsInvalidBreakGlassMaxDuration(t *testing.T) {
	t.Parallel()

	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Source: staticConfigSource{
			"ENVIRONMENT":              "development",
			"API_TLS_ENABLED":          "false",
			"BREAK_GLASS_MAX_DURATION": "nonsense",
		},
	})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}

	_, err = LoadAPIConfig(cm)
	if err == nil {
		t.Fatal("expected invalid break-glass duration error")
	}
}

func TestLoadAPIConfigRejectsNonPositiveBreakGlassMaxDuration(t *testing.T) {
	t.Parallel()

	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Source: staticConfigSource{
			"ENVIRONMENT":              "development",
			"API_TLS_ENABLED":          "false",
			"BREAK_GLASS_MAX_DURATION": "0",
		},
	})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}

	_, err = LoadAPIConfig(cm)
	if err == nil {
		t.Fatal("expected non-positive break-glass duration error")
	}
}
