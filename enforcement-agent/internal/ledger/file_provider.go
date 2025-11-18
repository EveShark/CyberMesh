package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Provider exposes the ledger snapshot interface.
type Provider interface {
	Snapshot(ctx context.Context) (map[string]policy.PolicySpec, error)
}

// FileProvider loads ledger policy specifications from a JSON snapshot file.
// The file is expected to contain an array of PolicySpec objects.
type FileProvider struct {
	path           string
	reloadInterval time.Duration

	mu         sync.RWMutex
	cached     map[string]policy.PolicySpec
	lastLoaded time.Time
}

// NewFileProvider constructs a FileProvider.
func NewFileProvider(path string, reloadInterval time.Duration) *FileProvider {
	return &FileProvider{path: path, reloadInterval: reloadInterval}
}

// Snapshot returns the latest ledger view, reloading the file when stale.
func (f *FileProvider) Snapshot(ctx context.Context) (map[string]policy.PolicySpec, error) {
	if f.path == "" {
		return nil, fmt.Errorf("ledger: snapshot path not configured")
	}

	f.mu.RLock()
	cached := f.cached
	stale := f.reloadInterval <= 0 || time.Since(f.lastLoaded) > f.reloadInterval
	f.mu.RUnlock()

	if cached != nil && !stale {
		return cloneMap(cached), nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	data, err := os.ReadFile(f.path)
	if err != nil {
		return nil, fmt.Errorf("ledger: read snapshot: %w", err)
	}

	var specs []policy.PolicySpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("ledger: parse snapshot: %w", err)
	}

	snapshot := make(map[string]policy.PolicySpec, len(specs))
	for _, spec := range specs {
		if spec.ID == "" {
			continue
		}
		snapshot[spec.ID] = spec
	}

	f.mu.Lock()
	f.cached = snapshot
	f.lastLoaded = time.Now().UTC()
	f.mu.Unlock()

	return cloneMap(snapshot), nil
}

func cloneMap(src map[string]policy.PolicySpec) map[string]policy.PolicySpec {
	if src == nil {
		return nil
	}
	clone := make(map[string]policy.PolicySpec, len(src))
	for k, v := range src {
		clone[k] = v
	}
	return clone
}
