package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIBasePath   = "/api/v1"
	maxResponseBodyBytes = 8 << 20
	headerRequestID      = "X-Request-ID"
	headerTenantID       = "X-Tenant-ID"
	headerWorkflowID     = "X-Workflow-Id"
	headerAPIKey         = "X-API-Key"
	headerIdempotency    = "Idempotency-Key"
)

type cliConfig struct {
	BaseURL         string
	Token           string
	APIKey          string
	TokenFile       string
	APIKeyFile      string
	Tenant          string
	Output          string
	InteractiveMode string
	MTLSCertFile    string
	MTLSKeyFile     string
	CAFile          string
	Timeout         time.Duration
}

type apiClient struct {
	baseURL *url.URL
	http    *http.Client
	cfg     cliConfig
}

type apiEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *apiError       `json:"error"`
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type requestOptions struct {
	Method      string
	Path        string
	Query       map[string]string
	Headers     map[string]string
	Body        any
	ExpectEmpty bool
}

func newAPIClient(cfg cliConfig) (*apiClient, error) {
	cfg.BaseURL = firstNonEmpty(cfg.BaseURL, defaultPluginBaseURL)
	normalized, err := normalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	httpClient, err := buildHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	return &apiClient{
		baseURL: normalized,
		http:    httpClient,
		cfg:     cfg,
	}, nil
}

func buildHTTPClient(cfg cliConfig) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if strings.TrimSpace(cfg.CAFile) != "" {
		caPEM, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse ca file: no certificates found")
		}
		tlsConfig.RootCAs = pool
	}

	if strings.TrimSpace(cfg.MTLSCertFile) != "" || strings.TrimSpace(cfg.MTLSKeyFile) != "" {
		if strings.TrimSpace(cfg.MTLSCertFile) == "" || strings.TrimSpace(cfg.MTLSKeyFile) == "" {
			return nil, fmt.Errorf("both --mtls-cert and --mtls-key are required together")
		}
		cert, err := tls.LoadX509KeyPair(cfg.MTLSCertFile, cfg.MTLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load mTLS keypair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	transport.TLSClientConfig = tlsConfig
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func normalizeBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("base url is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid base url: host is required")
	}
	cleanPath := strings.TrimSuffix(parsed.Path, "/")
	switch {
	case cleanPath == "":
		parsed.Path = defaultAPIBasePath
	case strings.HasSuffix(cleanPath, defaultAPIBasePath):
		parsed.Path = cleanPath
	default:
		parsed.Path = path.Clean(cleanPath + defaultAPIBasePath)
	}
	return parsed, nil
}

func (c *apiClient) do(ctx context.Context, opts requestOptions, out any) error {
	target := *c.baseURL
	rawPath := path.Clean(strings.TrimSuffix(c.baseURL.Path, "/") + "/" + strings.TrimPrefix(opts.Path, "/"))
	if unescaped, err := url.PathUnescape(rawPath); err == nil {
		target.Path = unescaped
		if rawPath != unescaped {
			target.RawPath = rawPath
		}
	} else {
		target.Path = rawPath
		target.RawPath = ""
	}
	query := target.Query()
	for key, value := range opts.Query {
		if strings.TrimSpace(value) != "" {
			query.Set(key, value)
		}
	}
	target.RawQuery = query.Encode()

	var body io.Reader
	if opts.Body != nil {
		payload, err := json.Marshal(opts.Body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, opts.Method, target.String(), body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if opts.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(c.cfg.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.cfg.Token))
	} else if strings.TrimSpace(c.cfg.APIKey) != "" {
		req.Header.Set(headerAPIKey, strings.TrimSpace(c.cfg.APIKey))
	}
	if strings.TrimSpace(c.cfg.Tenant) != "" {
		req.Header.Set(headerTenantID, strings.TrimSpace(c.cfg.Tenant))
	}
	for key, value := range opts.Headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if int64(len(payload)) > maxResponseBodyBytes {
		return fmt.Errorf("read response: body exceeds %d bytes", maxResponseBodyBytes)
	}
	if len(payload) == 0 {
		if resp.StatusCode >= 400 {
			return fmt.Errorf("request failed with status %d", resp.StatusCode)
		}
		return nil
	}

	var env apiEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		if resp.StatusCode >= 400 {
			return fmt.Errorf("decode error response: %w", err)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}

	if resp.StatusCode >= 400 || !env.Success {
		if env.Error != nil {
			code := sanitizeRemoteText(env.Error.Code)
			message := sanitizeRemoteText(env.Error.Message)
			requestID := sanitizeRemoteText(env.Error.RequestID)
			if detail := rateLimitDetail(resp.StatusCode, code, resp.Header); detail != "" {
				message = message + " (" + detail + ")"
			}
			if requestID != "" && !strings.EqualFold(requestID, "unknown") {
				return fmt.Errorf("%s: %s (request_id=%s)", code, message, requestID)
			}
			return fmt.Errorf("%s: %s", code, message)
		}
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	if opts.ExpectEmpty || out == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}

func sanitizeRemoteText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(r)
		case r >= 32 && r != 127:
			b.WriteRune(r)
		default:
			b.WriteRune('?')
		}
	}
	return strings.TrimSpace(b.String())
}

func rateLimitDetail(statusCode int, code string, header http.Header) string {
	if statusCode != http.StatusTooManyRequests &&
		!strings.Contains(code, "RATE_LIMIT") &&
		!strings.Contains(code, "COOLDOWN_ACTIVE") {
		return ""
	}

	retryAfter := strings.TrimSpace(header.Get("Retry-After"))
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
			return fmt.Sprintf("retry after %ds", seconds)
		}
		return "retry after " + retryAfter
	}

	reset := strings.TrimSpace(header.Get("X-RateLimit-Reset"))
	if reset == "" {
		return ""
	}
	resetUnix, err := strconv.ParseInt(reset, 10, 64)
	if err != nil || resetUnix <= 0 {
		return ""
	}
	wait := time.Until(time.Unix(resetUnix, 0))
	if wait <= 0 {
		return ""
	}
	seconds := int(wait.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("retry after %ds", seconds)
}
