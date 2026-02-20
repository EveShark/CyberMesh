package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Enabled       bool
	URL           string
	Username      string
	Password      string
	Timeout       time.Duration
	CacheTTL      time.Duration
	AllowFallback bool
}

type Client struct {
	cfg        Config
	httpClient *http.Client
	mu         sync.Mutex
	cache      map[string]cachedSchema
}

type cachedSchema struct {
	id        int
	schema    string
	expiresAt time.Time
}

func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		cache: make(map[string]cachedSchema),
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.cfg.Enabled
}

func (c *Client) SubjectLatestID(ctx context.Context, subject string) (int, error) {
	if !c.Enabled() {
		return 0, errors.New("registry disabled")
	}
	if subject == "" {
		return 0, errors.New("subject required")
	}
	if id, ok := c.getCachedID(subject); ok {
		return id, nil
	}
	url := fmt.Sprintf("%s/subjects/%s/versions/latest", strings.TrimRight(c.cfg.URL, "/"), subject)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	c.applyAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("registry status %d", resp.StatusCode)
	}
	var out struct {
		ID     int    `json:"id"`
		Schema string `json:"schema"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	c.setCached(subject, out.ID, out.Schema)
	return out.ID, nil
}

func (c *Client) SchemaByID(ctx context.Context, id int) (string, error) {
	if !c.Enabled() {
		return "", errors.New("registry disabled")
	}
	if id <= 0 {
		return "", errors.New("schema id required")
	}
	url := fmt.Sprintf("%s/schemas/ids/%d", strings.TrimRight(c.cfg.URL, "/"), id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	c.applyAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("registry status %d", resp.StatusCode)
	}
	var out struct {
		Schema string `json:"schema"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Schema, nil
}

func (c *Client) getCachedID(subject string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.cache[subject]
	if !ok || time.Now().After(item.expiresAt) {
		return 0, false
	}
	return item.id, true
}

func (c *Client) setCached(subject string, id int, schema string) {
	ttl := c.cfg.CacheTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	c.mu.Lock()
	c.cache[subject] = cachedSchema{
		id:        id,
		schema:    schema,
		expiresAt: time.Now().Add(ttl),
	}
	c.mu.Unlock()
}

func (c *Client) applyAuth(req *http.Request) {
	if c.cfg.Username != "" || c.cfg.Password != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
}
