// Package p2p provides the peer state, health and reputation tracking.
// It is independent from consensus and only concerned with network hygiene.
package p2p

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	// project-local packages
	"backend/pkg/config"
	"backend/pkg/utils"
)

// State tracks peer lifecycle, health and reputation. It is threadsafe and
// intentionally minimal so it can be reused by consensus and router validators.
type State struct {
	log       *utils.Logger
	cfg       *config.NodeConfig
	configMgr *utils.ConfigManager

	mu    sync.RWMutex
	peers map[peer.ID]*PeerState

	// Configuration (loaded from configMgr, no hardcoded values)
	heartbeatInterval time.Duration
	livenessTimeout   time.Duration
	decayInterval     time.Duration
	decayFactor       float64
	quarantineTTL     time.Duration
	maxPeers          int

	// Background maintenance
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics hooks (optional)
	metrics Metrics
}

// Metrics is a narrow interface to decouple from any metrics backend.
type Metrics interface {
	SetGauge(name string, v float64, labels map[string]string)
	IncCounter(name string, delta float64, labels map[string]string)
	ObserveHist(name string, v float64, labels map[string]string)
}

// PeerState holds rolling health/reputation for a peer.
type PeerState struct {
	ID           peer.ID
	LastSeen     time.Time
	LastDecay    time.Time
	BytesIn      uint64
	MsgIn        uint64
	Score        float64
	Quarantined  bool
	QuarantineAt time.Time
	Labels       map[string]string
}

// NewState constructs a State with parameters from configuration (no hardcoding).
func NewState(parentCtx context.Context, log *utils.Logger, cfg *config.NodeConfig, configMgr *utils.ConfigManager, metrics Metrics) *State {
	if log == nil {
		log = utils.GetLogger()
	}
	if configMgr == nil {
		panic("config manager is required for State")
	}

	ctx, cancel := context.WithCancel(parentCtx)

	// Load all configuration from configMgr with secure defaults
	hb := configMgr.GetDuration("P2P_HEARTBEAT_INTERVAL", 5*time.Second)
	live := configMgr.GetDuration("P2P_LIVENESS_TIMEOUT", 20*time.Second)
	decay := configMgr.GetDuration("P2P_REPUTATION_DECAY_INTERVAL", 30*time.Second)
	df := configMgr.GetFloat64("P2P_REPUTATION_DECAY_FACTOR", 0.98) // multiplicative decay
	qttl := configMgr.GetDuration("P2P_QUARANTINE_TTL", 5*time.Minute)
	maxp := configMgr.GetIntRange("P2P_MAX_PEERS", 512, 1, 10000)

	s := &State{
		log:               log,
		cfg:               cfg,
		configMgr:         configMgr,
		peers:             make(map[peer.ID]*PeerState),
		heartbeatInterval: hb,
		livenessTimeout:   live,
		decayInterval:     decay,
		decayFactor:       clamp(df, 0.80, 0.999),
		quarantineTTL:     qttl,
		maxPeers:          maxp,
		metrics:           metrics,
		ctx:               ctx,
		cancel:            cancel,
	}

	log.Info("P2P state manager created",
		utils.ZapDuration("heartbeat_interval", s.heartbeatInterval),
		utils.ZapDuration("liveness_timeout", s.livenessTimeout),
		utils.ZapDuration("decay_interval", s.decayInterval),
		utils.ZapFloat64("decay_factor", s.decayFactor),
		utils.ZapDuration("quarantine_ttl", s.quarantineTTL),
		utils.ZapInt("max_peers", s.maxPeers))

	return s
}

// Start begins background maintenance loops (decay, liveness).
func (s *State) Start() {
	s.wg.Add(1)
	go s.decayLoop()

	s.wg.Add(1)
	go s.livenessLoop()

	s.log.Info("P2P state manager background loops started")
}

// Stop gracefully shuts down background loops
func (s *State) Stop() {
	s.cancel()
	s.wg.Wait()
	s.log.Info("P2P state manager stopped")
}

// OnConnect marks a peer connected (called by router via notifiee).
func (s *State) OnConnect(pid peer.ID, labels map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := s.ensure(pid)
	ps.LastSeen = time.Now()
	ps.LastDecay = time.Now()
	if labels != nil {
		ps.Labels = labels
	}
	// small boost for successful handshake
	ps.Score += 0.2
	s.observeCounts()

	s.log.Debug("peer connected",
		utils.ZapString("peer_id", pid.String()),
		utils.ZapFloat64("score", ps.Score))
}

// OnDisconnect marks a peer disconnected.
func (s *State) OnDisconnect(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ps, ok := s.peers[pid]; ok {
		ps.LastSeen = time.Now()
	}
	s.observeCounts()

	s.log.Debug("peer disconnected", utils.ZapString("peer_id", pid.String()))
}

// OnMessage updates byte/msg counters and last seen.
func (s *State) OnMessage(topic string, from peer.ID, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := s.ensure(from)
	ps.LastSeen = time.Now()
	ps.MsgIn++
	ps.BytesIn += uint64(n)

	// Positive reinforcement for participation; capped to avoid runaway
	ps.Score = clamp(ps.Score+0.05, -100, 100)

	// Metrics
	if s.metrics != nil {
		s.metrics.IncCounter("p2p_messages_in_total", 1, map[string]string{"topic": topic})
		s.metrics.ObserveHist("p2p_message_size_bytes", float64(n), map[string]string{"topic": topic})
	}
	s.observeCounts()

	s.log.Debug("peer message received",
		utils.ZapString("peer_id", from.String()),
		utils.ZapString("topic", topic),
		utils.ZapInt("size", n),
		utils.ZapFloat64("score", ps.Score))
}

// Penalize applies a penalty for misbehavior (evidence from consensus layer).
func (s *State) Penalize(pid peer.ID, severity float64, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := s.ensure(pid)
	pen := clamp(severity, 0.1, 10.0)
	oldScore := ps.Score
	ps.Score -= pen

	quarantineThreshold := s.configMgr.GetFloat64("P2P_QUARANTINE_THRESHOLD", -5.0)
	if ps.Score < quarantineThreshold {
		s.quarantine(ps, reason)
	}
	s.observeCounts()

	s.log.Warn("peer penalized",
		utils.ZapString("peer_id", pid.String()),
		utils.ZapString("reason", reason),
		utils.ZapFloat64("severity", severity),
		utils.ZapFloat64("penalty", pen),
		utils.ZapFloat64("old_score", oldScore),
		utils.ZapFloat64("new_score", ps.Score),
		utils.ZapBool("quarantined", ps.Quarantined))
}

// IsQuarantined returns whether a peer is isolated.
func (s *State) IsQuarantined(pid peer.ID) bool {
	s.mu.RLock()
	ps, ok := s.peers[pid]
	s.mu.RUnlock()

	if !ok {
		return false
	}

	// Check quarantine status atomically
	isQuarantined := ps.Quarantined && time.Since(ps.QuarantineAt) < s.quarantineTTL

	return isQuarantined
}

// ScoreFor exposes current score to router (for GossipSub AppSpecificScore).
func (s *State) ScoreFor(pid peer.ID) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ps, ok := s.peers[pid]; ok {
		return ps.Score
	}
	return 0
}

// Snapshot returns a copy of peer states for diagnostics.
func (s *State) Snapshot() map[peer.ID]PeerState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[peer.ID]PeerState, len(s.peers))
	for id, ps := range s.peers {
		out[id] = *ps
	}
	return out
}

// GetPeerCount returns the number of tracked peers
func (s *State) GetPeerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers)
}

// GetConnectedPeerCount returns the number of non-quarantined peers.
func (s *State) GetConnectedPeerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, ps := range s.peers {
		if !ps.Quarantined {
			count++
		}
	}
	return count
}

// GetActivePeerCount returns the number of recently active peers.
func (s *State) GetActivePeerCount(since time.Duration) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, ps := range s.peers {
		if !ps.Quarantined && now.Sub(ps.LastSeen) < since {
			count++
		}
	}
	return count
}

// GetQuarantinedCount returns the number of quarantined peers
func (s *State) GetQuarantinedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, ps := range s.peers {
		if ps.Quarantined {
			count++
		}
	}
	return count
}

// --- internals ---

func (s *State) ensure(id peer.ID) *PeerState {
	// Check max peers limit
	if len(s.peers) >= s.maxPeers {
		s.evictLowestScorePeer()
	}

	if ps, ok := s.peers[id]; ok {
		return ps
	}
	ps := &PeerState{ID: id, LastSeen: time.Now(), LastDecay: time.Now(), Score: 0.0, Labels: map[string]string{}}
	s.peers[id] = ps
	return ps
}

func (s *State) decayLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.decayInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.applyDecay()
		}
	}
}

func (s *State) livenessLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkLiveness()
		}
	}
}

func (s *State) applyDecay() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	decayedCount := 0
	releasedCount := 0

	for _, ps := range s.peers {
		oldScore := ps.Score

		// Calculate time since last decay application
		timeSinceDecay := now.Sub(ps.LastDecay)
		if timeSinceDecay < s.decayInterval {
			continue // not time yet
		}

		// Apply decay for elapsed intervals
		steps := float64(timeSinceDecay) / float64(s.decayInterval)
		if steps > 0 {
			ps.Score *= math.Pow(s.decayFactor, steps)
			ps.LastDecay = now
			if math.Abs(oldScore-ps.Score) > 0.01 {
				decayedCount++
			}
		}

		// auto-unquarantine after TTL
		if ps.Quarantined && now.Sub(ps.QuarantineAt) >= s.quarantineTTL {
			ps.Quarantined = false
			releasedCount++
			s.log.Info("peer released from quarantine",
				utils.ZapString("peer_id", ps.ID.String()),
				utils.ZapDuration("quarantine_duration", now.Sub(ps.QuarantineAt)))
		}
	}

	if decayedCount > 0 || releasedCount > 0 {
		s.log.Debug("reputation decay applied",
			utils.ZapInt("decayed_peers", decayedCount),
			utils.ZapInt("released_peers", releasedCount))
	}

	s.observeCounts()
}

func (s *State) checkLiveness() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	staleCount := 0
	quarantineThreshold := s.configMgr.GetFloat64("P2P_QUARANTINE_THRESHOLD", -5.0)

	for _, ps := range s.peers {
		if now.Sub(ps.LastSeen) > s.livenessTimeout {
			// gentle penalty for being silent
			oldScore := ps.Score
			ps.Score -= 0.5
			staleCount++

			if ps.Score < quarantineThreshold && !ps.Quarantined {
				s.quarantine(ps, "liveness-timeout")
			}

			s.log.Debug("peer liveness penalty applied",
				utils.ZapString("peer_id", ps.ID.String()),
				utils.ZapFloat64("old_score", oldScore),
				utils.ZapFloat64("new_score", ps.Score),
				utils.ZapDuration("silence_duration", now.Sub(ps.LastSeen)))
		}
	}

	if staleCount > 0 {
		s.log.Debug("liveness check completed", utils.ZapInt("stale_peers", staleCount))
	}

	s.observeCounts()
}

func (s *State) quarantine(ps *PeerState, reason string) {
	ps.Quarantined = true
	ps.QuarantineAt = time.Now()

	if s.metrics != nil {
		s.metrics.IncCounter("p2p_quarantine_events_total", 1, map[string]string{"reason": reason})
	}

	s.log.Warn("peer quarantined",
		utils.ZapString("peer_id", ps.ID.String()),
		utils.ZapString("reason", reason),
		utils.ZapFloat64("score", ps.Score),
		utils.ZapDuration("ttl", s.quarantineTTL))
}

// metrics helpers
func (s *State) observeCounts() {
	if s.metrics == nil {
		return
	}
	active := 0
	quarantined := 0
	for _, ps := range s.peers {
		if !ps.Quarantined {
			active++
		} else {
			quarantined++
		}
	}
	s.metrics.SetGauge("p2p_active_peers", float64(active), nil)
	s.metrics.SetGauge("p2p_quarantined_peers", float64(quarantined), nil)
	s.metrics.SetGauge("p2p_known_peers", float64(len(s.peers)), nil)
}

// evictLowestScorePeer removes the lowest-scoring non-quarantined peer
func (s *State) evictLowestScorePeer() {
	var lowestID peer.ID
	lowestScore := math.MaxFloat64

	for id, ps := range s.peers {
		// Don't evict quarantined peers (let them expire naturally)
		if ps.Quarantined {
			continue
		}
		if ps.Score < lowestScore {
			lowestScore = ps.Score
			lowestID = id
		}
	}

	if lowestID != "" {
		delete(s.peers, lowestID)
		s.log.Info("evicted peer due to max limit",
			utils.ZapString("peer_id", lowestID.String()),
			utils.ZapFloat64("score", lowestScore),
			utils.ZapInt("peer_count", len(s.peers)))
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
