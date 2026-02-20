package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Config struct {
	Enabled  bool
	URL      string
	Username string
	Password string
	Timeout  time.Duration
	CacheTTL time.Duration
}

type Client struct {
	cfg        Config
	cacheMu    sync.RWMutex
	cache      map[string]cacheEntry
	httpClient *http.Client
}

type cacheEntry struct {
	id        int
	expiresAt time.Time
}

func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	return &Client{
		cfg:        cfg,
		cache:      make(map[string]cacheEntry),
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.cfg.Enabled && c.cfg.URL != ""
}

func (c *Client) SubjectLatestID(ctx context.Context, subject string) (int, error) {
	if !c.Enabled() {
		return 0, errors.New("registry disabled")
	}
	if subject == "" {
		return 0, errors.New("subject required")
	}
	if id, ok := c.cachedID(subject); ok {
		return id, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/subjects/%s/versions/latest", c.cfg.URL, subject), nil)
	if err != nil {
		return 0, err
	}
	if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("registry status %d", resp.StatusCode)
	}
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	c.storeID(subject, payload.ID)
	return payload.ID, nil
}

func (c *Client) cachedID(subject string) (int, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	entry, ok := c.cache[subject]
	if !ok || time.Now().After(entry.expiresAt) {
		return 0, false
	}
	return entry.id, true
}

func (c *Client) storeID(subject string, id int) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.cache[subject] = cacheEntry{id: id, expiresAt: time.Now().Add(c.cfg.CacheTTL)}
}

func ParseSchemaID(headers map[string][]byte) int {
	if headers == nil {
		return 0
	}
	if val, ok := headers["schema_id"]; ok {
		if id, err := strconv.Atoi(string(val)); err == nil {
			return id
		}
	}
	return 0
}
