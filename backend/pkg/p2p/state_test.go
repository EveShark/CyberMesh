package p2p

import (
	"context"
	"sync"
	"testing"
	"time"

	"backend/pkg/config"
	"backend/pkg/utils"
	"github.com/libp2p/go-libp2p/core/peer"
)

type testMapSource struct {
	mu     sync.RWMutex
	values map[string]string
}

func (m *testMapSource) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.values[key]
	return v, ok
}

func (m *testMapSource) Set(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
	return nil
}

func (m *testMapSource) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}

func (m *testMapSource) List() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.values))
	for k, v := range m.values {
		out[k] = v
	}
	return out
}

func newTestConfigManager(t *testing.T) *utils.ConfigManager {
	t.Helper()
	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Source: &testMapSource{values: map[string]string{}},
	})
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}
	return cm
}

func TestStateTouchPeerUpdatesLastSeen(t *testing.T) {
	cm := newTestConfigManager(t)
	state := NewState(context.Background(), nil, &config.NodeConfig{}, cm, nil)
	pid := peer.ID("12D3KooWTestPeer111111111111111111111111")
	state.OnConnect(pid, nil)

	state.mu.Lock()
	if ps, ok := state.peers[pid]; ok {
		ps.LastSeen = time.Now().Add(-45 * time.Second)
	}
	state.mu.Unlock()

	if got := state.GetActivePeerCount(20 * time.Second); got != 0 {
		t.Fatalf("expected inactive peer count, got %d", got)
	}

	state.TouchPeer(pid, time.Now())
	if got := state.GetActivePeerCount(20 * time.Second); got != 1 {
		t.Fatalf("expected active peer count after touch, got %d", got)
	}
}

func TestStateTouchPeerSkipsUnknownPeer(t *testing.T) {
	cm := newTestConfigManager(t)
	state := NewState(context.Background(), nil, &config.NodeConfig{}, cm, nil)
	unknown := peer.ID("12D3KooWUnknownPeer111111111111111111111")
	state.TouchPeer(unknown, time.Now())

	if count := state.GetPeerCount(); count != 0 {
		t.Fatalf("expected no peers to be added, got %d", count)
	}
}
