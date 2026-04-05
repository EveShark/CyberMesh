package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

type Enforcer struct {
	baseURL        string
	commandURL     string
	healthURL      string
	bearerToken    string
	httpClient     *http.Client
	allowedActions map[string]struct{}
	log            *zap.Logger
	mu             sync.RWMutex
	set            map[string]policy.PolicySpec
}

func New(cfg Config, logger *zap.Logger) (*Enforcer, error) {
	cfg = cfg.withDefaults()
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("app backend: APP_ENFORCER_BASE_URL is required")
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("app backend: invalid base url: %w", err)
	}
	if base.Scheme != "https" && base.Scheme != "http" {
		return nil, fmt.Errorf("app backend: unsupported url scheme %q", base.Scheme)
	}
	commandURL, err := resolveURL(base, cfg.CommandPath)
	if err != nil {
		return nil, fmt.Errorf("app backend: invalid command path: %w", err)
	}
	healthURL, err := resolveURL(base, cfg.HealthPath)
	if err != nil {
		return nil, fmt.Errorf("app backend: invalid health path: %w", err)
	}

	actions := make(map[string]struct{}, len(cfg.AllowedActions))
	for _, action := range cfg.AllowedActions {
		action = strings.ToLower(strings.TrimSpace(action))
		if action == "" {
			continue
		}
		actions[action] = struct{}{}
	}
	if len(actions) == 0 {
		return nil, fmt.Errorf("app backend: allowed action list is empty")
	}

	return &Enforcer{
		baseURL:        cfg.BaseURL,
		commandURL:     commandURL,
		healthURL:      healthURL,
		bearerToken:    strings.TrimSpace(cfg.BearerToken),
		httpClient:     &http.Client{Timeout: cfg.Timeout},
		allowedActions: actions,
		log:            logger,
		set:            make(map[string]policy.PolicySpec),
	}, nil
}

func (e *Enforcer) Apply(ctx context.Context, spec policy.PolicySpec) error {
	policyID := strings.TrimSpace(spec.ID)
	if policyID == "" {
		return fmt.Errorf("app backend: policy id is required")
	}
	action := strings.ToLower(strings.TrimSpace(spec.Action))
	if action == "" {
		return fmt.Errorf("app backend: action is required")
	}
	if !e.isActionAllowed(action) {
		return fmt.Errorf("app backend: unsupported action %s", action)
	}

	payload := map[string]any{
		"schema_version":    "enforcement.adapter.command.v1",
		"policy_id":         policyID,
		"action":            action,
		"rule_type":         strings.TrimSpace(spec.RuleType),
		"scope_identifier":  policy.ScopeIdentifier(spec),
		"tenant":            effectiveTenant(spec),
		"region":            effectiveRegion(spec),
		"target":            spec.Target,
		"guardrails":        spec.Guardrails,
		"trace_id":          extractSpecString(spec.Raw, "trace_id"),
		"request_id":        extractSpecString(spec.Raw, "request_id"),
		"command_id":        extractSpecString(spec.Raw, "command_id"),
		"workflow_id":       extractSpecString(spec.Raw, "workflow_id"),
		"source_event_id":   extractSpecString(spec.Raw, "source_event_id"),
		"sentinel_event_id": extractSpecString(spec.Raw, "sentinel_event_id"),
	}

	if err := e.postJSON(ctx, e.commandURL, payload); err != nil {
		return err
	}

	e.mu.Lock()
	e.set[policyID] = spec
	e.mu.Unlock()
	return nil
}

func (e *Enforcer) Remove(ctx context.Context, policyID string) error {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return fmt.Errorf("app backend: policy id is required")
	}
	if !e.isActionAllowed("remove") {
		return fmt.Errorf("app backend: unsupported action remove")
	}
	payload := map[string]any{
		"schema_version": "enforcement.adapter.command.v1",
		"policy_id":      policyID,
		"action":         "remove",
	}
	if err := e.postJSON(ctx, e.commandURL, payload); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.set, policyID)
	e.mu.Unlock()
	return nil
}

func (e *Enforcer) List(context.Context) ([]policy.PolicySpec, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]policy.PolicySpec, 0, len(e.set))
	for _, spec := range e.set {
		out = append(out, spec)
	}
	return out, nil
}

func (e *Enforcer) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.healthURL, nil)
	if err != nil {
		return fmt.Errorf("app backend: build health request: %w", err)
	}
	if e.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.bearerToken)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("app backend: health request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("app backend: health status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (e *Enforcer) ReadyCheck(ctx context.Context) error { return e.HealthCheck(ctx) }

func (e *Enforcer) isActionAllowed(action string) bool {
	_, ok := e.allowedActions[action]
	return ok
}

func (e *Enforcer) postJSON(ctx context.Context, endpoint string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("app backend: encode payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("app backend: build command request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.bearerToken)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("app backend: command request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("app backend: command status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

func resolveURL(base *url.URL, rel string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rel))
	if err != nil {
		return "", err
	}
	// Security: command/health path must stay on the configured base host.
	if parsed.IsAbs() || strings.TrimSpace(parsed.Host) != "" {
		return "", fmt.Errorf("absolute URL overrides are not allowed")
	}
	return base.ResolveReference(parsed).String(), nil
}

func effectiveTenant(spec policy.PolicySpec) string {
	if spec.Target.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Tenant))
	}
	return strings.ToLower(strings.TrimSpace(spec.Tenant))
}

func effectiveRegion(spec policy.PolicySpec) string {
	if spec.Target.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Region))
	}
	return strings.ToLower(strings.TrimSpace(spec.Region))
}

func extractSpecString(raw map[string]any, key string) string {
	if raw == nil || strings.TrimSpace(key) == "" {
		return ""
	}
	if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if metadata, ok := raw["metadata"].(map[string]any); ok {
		if v, ok := metadata[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if trace, ok := raw["trace"].(map[string]any); ok {
		if key == "trace_id" {
			if v, ok := trace["id"].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		if v, ok := trace[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if params, ok := raw["params"].(map[string]any); ok {
		if v, ok := params[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
