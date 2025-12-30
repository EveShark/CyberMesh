// Package p2p implements the network layer: secure transport, discovery and routing.
// It is intentionally thin and configurable, delegating trust & policy to state.go
// and cluster parameters to the config package.
package p2p

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	mdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	ping "github.com/libp2p/go-libp2p/p2p/protocol/ping"
	noise "github.com/libp2p/go-libp2p/p2p/security/noise"
	tlsp2p "github.com/libp2p/go-libp2p/p2p/security/tls"
	multiaddr "github.com/multiformats/go-multiaddr"

	// project-local packages
	"backend/pkg/config"
	"backend/pkg/consensus/types"
	"backend/pkg/utils"
)

// Handler is the callback signature for delivered messages on a topic.
type Handler func(ctx context.Context, from peer.ID, data []byte) error

// Router encapsulates the networking primitives (host, gossip, discovery).
// It is intentionally stateless with regards to trust; see State in state.go.
type Router struct {
	ctx    context.Context
	cancel context.CancelFunc
	log    *utils.Logger

	Host      host.Host
	DHT       *dht.IpfsDHT
	Gossip    *pubsub.PubSub
	Topics    map[string]*pubsub.Topic
	Subs      map[string]*pubsub.Subscription
	Handlers  map[string][]Handler
	Discovery discovery.Discovery

	nodeCfg   *config.NodeConfig
	secCfg    *config.SecurityConfig
	configMgr *utils.ConfigManager
	state     *State // for peer lifecycle callbacks & scoring

	opts       RouterOptions
	mu         sync.RWMutex
	handlerSem chan struct{} // semaphore for handler concurrency

	// default topic validator used for all subscriptions
	topicValidator func(ctx context.Context, id peer.ID, msg *pubsub.Message) pubsub.ValidationResult

	bytesPublished atomic.Uint64

	trendMu              sync.RWMutex
	trendSamples         []RouterTrendSample
	trendWindow          time.Duration
	trendMaxSamples      int
	pingService          *ping.PingService
	latencyMu            sync.RWMutex
	peerLatency          map[peer.ID]time.Duration
	latencyProbeInterval time.Duration
	latencyProbeTimeout  time.Duration
	throughputMu         sync.Mutex
	lastBytesReceived    uint64
	lastBytesSent        uint64
	lastSampleTime       time.Time
	peerRateSnapshots    map[peer.ID]peerRateSample
	reportedAdjacency    map[peer.ID]reportedEdges
	reportedExpiry       time.Duration
	adjMu                sync.RWMutex
	adjTopic             string
	adjBroadcastInterval time.Duration
	adjMaxEdges          int
	observationsExpiry   time.Duration
	peerObservations     map[peer.ID]observedPeerStat
}

// RouterStats summarizes live network telemetry for API exposure.
type RouterStats struct {
	PeerCount       int
	InboundPeers    int
	OutboundPeers   int
	BytesReceived   uint64
	BytesSent       uint64
	AvgLatencyMs    float64
	Timestamp       time.Time
	History         []RouterTrendSample
	Peers           []RouterPeerStats
	InboundRateBps  float64
	OutboundRateBps float64
	Edges           []RouterEdge
	Self            peer.ID
}

// RouterTrendSample captures a historical snapshot of router telemetry.
type RouterTrendSample struct {
	Timestamp     time.Time
	PeerCount     int
	InboundPeers  int
	OutboundPeers int
	AvgLatencyMs  float64
	BytesReceived uint64
	BytesSent     uint64
}

// RouterPeerStats captures per-peer transport metrics for diagnostics.
type RouterPeerStats struct {
	ID        peer.ID
	LatencyMs float64
	BytesIn   uint64
	BytesOut  uint64
	LastSeen  time.Time
	Status    string
	Labels    map[string]string
	RateBps   float64
}

// RouterEdge represents an adjacency between the local node and a peer.
type RouterEdge struct {
	Source     peer.ID
	Target     peer.ID
	Direction  string
	Status     string
	Confidence string
	ReportedBy peer.ID
	UpdatedAt  time.Time
}

type peerRateSample struct {
	bytesIn uint64
}

type reportedEdges struct {
	Edges     []RouterEdge
	Collected time.Time
}

type observedPeerStat struct {
	Stats     RouterPeerStats
	Collected time.Time
}

type adjacencyPayload struct {
	Reporter   string                 `json:"reporter"`
	ObservedAt int64                  `json:"observed_at"`
	Edges      []adjacencyEdgePayload `json:"edges,omitempty"`
	Peers      []adjacencyPeerPayload `json:"peers,omitempty"`
}

type adjacencyEdgePayload struct {
	Target     string `json:"target"`
	Direction  string `json:"direction,omitempty"`
	Status     string `json:"status,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	UpdatedAt  int64  `json:"updated_at,omitempty"`
}

type adjacencyPeerPayload struct {
	Peer      string  `json:"peer"`
	Status    string  `json:"status,omitempty"`
	RateBps   float64 `json:"rate_bps,omitempty"`
	LatencyMs float64 `json:"latency_ms,omitempty"`
	LastSeen  int64   `json:"last_seen,omitempty"`
	BytesIn   uint64  `json:"bytes_in,omitempty"`
}

// RouterOptions allows fine-grained tuning without hardcoding.
type RouterOptions struct {
	// Topics to create/subscribe.
	Topics []string
	// Rendezvous namespace for discovery (e.g., "cybermesh/v1/prod").
	Rendezvous string
	// Protocol prefix for DHT and pubsub.
	ProtocolPrefix string
	// PreferNoise selects Noise as primary transport security (default true).
	PreferNoise bool
	// EnableTLS enables TLS 1.3 as an additional security transport.
	EnableTLS bool
	// EnableMDNS enables local LAN discovery for dev/test.
	EnableMDNS bool
	// ConnManager bounds (low/high water)
	ConnLow, ConnHigh int
	// Gossip parameters override (nil = defaults). AppSpecificScore will be wired to State.ScoreFor.
	GossipParams *pubsub.PeerScoreParams
	// Topic max size validation (bytes), map per topic.
	TopicMaxSize map[string]int
	// Optional explicit bootstrap multiaddrs (overrides config Mesh BootstrapNodes).
	BootstrapAddrs []string
}

// NewRouter constructs a Router using only configuration + environment.
// All secrets/keys are derived from SecurityConfig or config manager (no hardcoded keys).
func NewRouter(parent context.Context, nodeCfg *config.NodeConfig, secCfg *config.SecurityConfig, st *State, log *utils.Logger, configMgr *utils.ConfigManager, opts RouterOptions) (*Router, error) {
	if nodeCfg == nil || secCfg == nil || log == nil || configMgr == nil {
		return nil, fmt.Errorf("nil inputs: nodeCfg=%v secCfg=%v log=%v configMgr=%v", nodeCfg != nil, secCfg != nil, log != nil, configMgr != nil)
	}
	ctx, cancel := context.WithCancel(parent)

	// ---- Identity (deterministic, from secret material or config) ----
	priv, pid, err := deriveIdentity(secCfg, configMgr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("derive identity: %w", err)
	}
	log.Info("p2p identity derived", utils.ZapString("peer_id", pid.String()))

	// ---- Allowed IPs / Trusted peers ----
	allowedNets := utils.ParseCIDRs(configMgr.GetStringSlice("ALLOWED_IPS", []string{}))
	trustedPeerIDs := parsePeerIDs(configMgr.GetStringSlice("TRUSTED_P2P_PEERS", []string{}))
	gater := &connGater{
		allowed: allowedNets,
		trusted: trustedPeerIDs,
		state:   st,
		log:     log,
	}

	// ---- Security Transports ----
	var secOpts []libp2p.Option
	if opts.PreferNoise || (!opts.EnableTLS) {
		secOpts = append(secOpts, libp2p.Security(noise.ID, noise.New))
	}
	if opts.EnableTLS {
		secOpts = append(secOpts, libp2p.Security(tlsp2p.ID, tlsp2p.New))
	}

	// ---- Listen addresses ----
	listenPort := nodeCfg.NodePort
	if listenPort <= 0 {
		listenPort = configMgr.GetIntRange("P2P_LISTEN_PORT", 8000, 1, 65535)
	}
	listenAddrs := []string{
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort),
	}
	if hasIPv6() {
		listenAddrs = append(listenAddrs, fmt.Sprintf("/ip6/::/tcp/%d", listenPort))
	}

	// ---- Connection manager ----
	// For small validator networks (5-10 nodes), use conservative limits
	// to prevent aggressive pruning of peer connections
	low, high := opts.ConnLow, opts.ConnHigh
	if low <= 0 || high <= 0 || high <= low {
		// Default to validator network size + buffer
		low = configMgr.GetIntRange("P2P_CONN_LOW", 4, 1, 1000)
		high = configMgr.GetIntRange("P2P_CONN_HIGH", 10, low+1, 2000)
	}
	// Longer grace period for validator networks to prevent spurious disconnects
	gracePeriod := configMgr.GetDuration("P2P_CONN_GRACE_PERIOD", 60*time.Second)
	cm, err := connmgr.NewConnManager(low, high, connmgr.WithGracePeriod(gracePeriod))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connmgr: %w", err)
	}

	// ---- Host ----
	// Combine all options (can't spread slice in variadic call)
	hostOpts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(listenAddrs...),
		libp2p.ConnectionManager(cm),
		libp2p.ConnectionGater(gater),
	}
	hostOpts = append(hostOpts, secOpts...)

	host, err := libp2p.New(hostOpts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("libp2p host: %w", err)
	}

	// ---- Routing (DHT) ----
	protoPrefix := opts.ProtocolPrefix
	if protoPrefix == "" {
		protoPrefix = configMgr.GetString("P2P_PROTOCOL_PREFIX", "/cybermesh")
	}
	dhtOpts := []dht.Option{
		dht.ProtocolPrefix(protocol.ID(protoPrefix + "/kad")),
		dht.Mode(dht.ModeAuto),
		dht.BootstrapPeers(), // we also dial configured bootstrap later
	}
	ipfsDHT, err := dht.New(ctx, host, dhtOpts...)
	if err != nil {
		_ = host.Close()
		cancel()
		return nil, fmt.Errorf("dht: %w", err)
	}

	// ---- Discovery (routing-based + optional MDNS) ----
	rd := drouting.NewRoutingDiscovery(ipfsDHT)
	rendezvous := opts.Rendezvous
	if rendezvous == "" {
		rendezvous = configMgr.GetString("RENDEZVOUS_NS", "cybermesh/default")
	}

	// Enable mDNS for local peer discovery
	if opts.EnableMDNS {
		mdnsService := mdns.NewMdnsService(host, rendezvous, &mdnsNotifee{h: host, log: log})
		if err := mdnsService.Start(); err != nil {
			log.Warn("mDNS service failed to start", utils.ZapError(err))
		} else {
			log.Info("mDNS local discovery enabled", utils.ZapString("rendezvous", rendezvous))
		}
	}

	// advertise & start finding peers in background
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_, _ = rd.Advertise(ctx, rendezvous)
			}
		}
	}()

	// ---- GossipSub v1.1 with scoring ----
	psOpts := []pubsub.Option{
		pubsub.WithMessageSigning(true),
		pubsub.WithStrictSignatureVerification(true),
	}

	// Configure GossipSub parameters for small validator networks
	defaultParams := pubsub.DefaultGossipSubParams()
	defaultParams.D = 3     // desired mesh size (3 of 4 other peers)
	defaultParams.Dlo = 2   // low watermark
	defaultParams.Dhi = 4   // high watermark (all 4 other peers)
	defaultParams.Dlazy = 4 // gossip target
	defaultParams.HeartbeatInterval = time.Second
	psOpts = append(psOpts, pubsub.WithGossipSubParams(defaultParams))
	score := &pubsub.PeerScoreParams{
		// plumb app-specific trust from State
		AppSpecificScore: func(p peer.ID) float64 {
			if st == nil {
				return 0
			}
			return st.ScoreFor(p)
		},
		DecayInterval: 10 * time.Second,
		DecayToZero:   0.01,
		RetainScore:   15 * time.Minute,

		// IP colocation penalties disabled for validator networks
		// In production, validators are trusted; in development, all run on localhost
		IPColocationFactorWeight:    0,
		IPColocationFactorThreshold: 100,
		BehaviourPenaltyWeight:      -10,
		BehaviourPenaltyThreshold:   10,
		BehaviourPenaltyDecay:       0.9,
		Topics:                      make(map[string]*pubsub.TopicScoreParams),
	}
	thresholds := &pubsub.PeerScoreThresholds{
		GossipThreshold:             -100,
		PublishThreshold:            -200,
		GraylistThreshold:           -500,
		AcceptPXThreshold:           5,
		OpportunisticGraftThreshold: 10,
	}
	if opts.GossipParams != nil {
		// merge: keep our AppSpecificScore hook
		score.Topics = opts.GossipParams.Topics
	}
	ps, err := pubsub.NewGossipSub(ctx, host, append(psOpts, pubsub.WithPeerScore(score, thresholds))...)
	if err != nil {
		_ = ipfsDHT.Close()
		_ = host.Close()
		cancel()
		return nil, fmt.Errorf("gossipsub: %w", err)
	}

	r := &Router{
		ctx:                  ctx,
		cancel:               cancel,
		log:                  log,
		Host:                 host,
		DHT:                  ipfsDHT,
		Gossip:               ps,
		Topics:               map[string]*pubsub.Topic{},
		Subs:                 map[string]*pubsub.Subscription{},
		Handlers:             map[string][]Handler{},
		Discovery:            rd,
		nodeCfg:              nodeCfg,
		secCfg:               secCfg,
		configMgr:            configMgr,
		state:                st,
		opts:                 opts,
		peerLatency:          make(map[peer.ID]time.Duration),
		peerRateSnapshots:    make(map[peer.ID]peerRateSample),
		reportedAdjacency:    make(map[peer.ID]reportedEdges),
		reportedExpiry:       configMgr.GetDuration("P2P_REPORTED_EDGE_TTL", 60*time.Second),
		adjTopic:             configMgr.GetString("P2P_ADJ_TOPIC", "cybermesh/p2p/adjacency"),
		adjBroadcastInterval: configMgr.GetDuration("P2P_ADJ_BROADCAST_INTERVAL", 20*time.Second),
		adjMaxEdges:          configMgr.GetIntRange("P2P_ADJ_MAX_EDGES", 128, 1, 2048),
		observationsExpiry:   configMgr.GetDuration("P2P_REPORTED_METRIC_TTL", 45*time.Second),
		peerObservations:     make(map[peer.ID]observedPeerStat),
	}
	r.reportedExpiry = clampDuration(r.reportedExpiry, 10*time.Second, 5*time.Minute)
	if r.adjTopic == "" {
		r.adjTopic = "cybermesh/p2p/adjacency"
	}
	r.adjBroadcastInterval = clampDuration(r.adjBroadcastInterval, 5*time.Second, 2*time.Minute)
	if r.adjMaxEdges <= 0 {
		r.adjMaxEdges = 128
	}
	r.observationsExpiry = clampDuration(r.observationsExpiry, 5*time.Second, 5*time.Minute)
	r.pingService = ping.NewPingService(host)
	r.latencyProbeInterval = configMgr.GetDuration("P2P_LATENCY_PROBE_INTERVAL", 30*time.Second)
	if r.latencyProbeInterval <= 0 {
		r.latencyProbeInterval = 30 * time.Second
	}
	r.latencyProbeTimeout = configMgr.GetDuration("P2P_LATENCY_PROBE_TIMEOUT", 5*time.Second)
	if r.latencyProbeTimeout <= 0 {
		r.latencyProbeTimeout = 5 * time.Second
	}

	if r.state != nil {
		r.state.ResetPeersSeen()
	}

	// Create semaphore for bounded handler concurrency
	maxHandlers := configMgr.GetIntRange("P2P_MAX_HANDLER_GOROUTINES", 200, 10, 1000)
	r.handlerSem = make(chan struct{}, maxHandlers)
	r.trendWindow = configMgr.GetDuration("P2P_TREND_WINDOW", 10*time.Minute)
	r.trendMaxSamples = configMgr.GetIntRange("P2P_TREND_SAMPLES", 360, 10, 5000)

	// ---- Network notifiee → update State on conn events ----
	host.Network().Notify(&netNotifiee{r: r})

	// ---- Topic validators (drop quarantined peers, oversize msgs) ----
	validator := func(ctx context.Context, id peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		if st != nil && st.IsQuarantined(id) {
			return pubsub.ValidationReject
		}
		if msg.Topic == nil {
			return pubsub.ValidationIgnore
		}
		if max, ok := opts.TopicMaxSize[*msg.Topic]; ok && len(msg.Data) > max {
			return pubsub.ValidationReject
		}
		return pubsub.ValidationAccept
	}
	r.topicValidator = validator

	// subscribe to initial topics (from opts, or config)
	topics := opts.Topics
	if len(topics) == 0 {
		topics = configMgr.GetStringSlice("P2P_TOPICS", []string{})
	}
	// Expand "consensus" into known subtopics to avoid topic mismatch when engine publishes subtopics
	if containsTopic(topics, "consensus") {
		topics = dedupeTopics(append(topics, []string{
			"consensus/proposal",
			"consensus/vote",
			"consensus/viewchange",
			"consensus/newview",
			"consensus/heartbeat",
			"consensus/evidence",
		}...))
	} else {
		topics = dedupeTopics(topics)
	}
	// Don't subscribe yet - let AttachConsensusHandlers do it to avoid race
	// Just register topic validators for later
	for _, t := range topics {
		_ = ps.RegisterTopicValidator(t, validator)
	}

	// ---- Bootstrap dialing (best-effort; discovery backfills) ----
	if err := r.dialBootstrapPeers(); err != nil {
		log.Warn("bootstrap dialing issues", utils.ZapError(err))
	}

	// ---- Discovery loop: find peers periodically ----
	go r.discoveryLoop(rendezvous)

	// Audit router initialization
	if secCfg.AuditLogger != nil {
		secCfg.AuditLogger.Security("p2p_router_initialized", map[string]interface{}{
			"node_id":     nodeCfg.NodeID,
			"peer_id":     pid.String(),
			"listen_port": listenPort,
		})
	}

	if r.pingService != nil {
		go r.monitorPeerLatency()
	}

	if r.Gossip != nil && r.adjTopic != "" {
		if err := r.Subscribe(r.adjTopic, r.handleAdjacencyMessage); err != nil {
			r.log.Warn("adjacency gossip subscribe failed", utils.ZapError(err))
		} else {
			go r.adjacencyBroadcastLoop()
		}
	}

	return r, nil
}

// RegisterHandler attaches a handler for a topic (called after basic router validation).
func (r *Router) RegisterHandler(topic string, h Handler) error {
	if topic == "" || h == nil {
		return fmt.Errorf("invalid handler registration")
	}
	r.mu.Lock()
	r.Handlers[topic] = append(r.Handlers[topic], h)
	r.mu.Unlock()
	return nil
}

// Subscribe ensures a topic exists and optionally installs a handler.
func (r *Router) Subscribe(topic string, handler Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.Topics[topic]; !ok {
		t, err := r.Gossip.Join(topic)
		if err != nil {
			return fmt.Errorf("join topic %s: %w", topic, err)
		}
		r.Topics[topic] = t
	}
	// Ensure validator is registered for dynamically subscribed topics
	if r.topicValidator != nil {
		_ = r.Gossip.RegisterTopicValidator(topic, r.topicValidator)
	}
	if _, ok := r.Subs[topic]; !ok {
		sub, err := r.Topics[topic].Subscribe()
		if err != nil {
			return fmt.Errorf("subscribe topic %s: %w", topic, err)
		}
		r.Subs[topic] = sub
		go r.DebugConsume(topic, sub)
	}
	if handler != nil {
		r.Handlers[topic] = append(r.Handlers[topic], handler)
	}

	// Audit topic subscription
	if r.secCfg.AuditLogger != nil {
		r.secCfg.AuditLogger.Info("p2p_topic_subscribed", map[string]interface{}{
			"topic":   topic,
			"node_id": r.nodeCfg.NodeID,
		})
	}

	return nil
}

// Publish broadcasts a message to a topic.
func (r *Router) Publish(topic string, data []byte) error {
	r.mu.RLock()
	t, ok := r.Topics[topic]
	subscribers := 0
	if t != nil {
		subscribers = len(t.ListPeers())
	}
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("topic %s not joined", topic)
	}

	// Log genesis publish attempts with subscriber count
	if strings.Contains(topic, "genesis") {
		r.log.Info("[GENESIS] attempting to publish",
			utils.ZapString("topic", topic),
			utils.ZapInt("subscribers", subscribers),
			utils.ZapInt("msg_bytes", len(data)))
	}

	// Validate message size
	maxSize := r.configMgr.GetIntRange("P2P_MAX_MESSAGE_SIZE", 1024*1024, 1024, 10*1024*1024)
	if len(data) > maxSize {
		return fmt.Errorf("message size %d exceeds maximum %d", len(data), maxSize)
	}

	err := t.Publish(r.ctx, data)

	if err != nil {
		r.log.Warn("publish failed", utils.ZapString("topic", topic), utils.ZapError(err))
	} else {
		// Log successful publish for genesis debugging
		if strings.Contains(topic, "genesis") {
			r.log.Info("[GENESIS] message published successfully",
				utils.ZapString("topic", topic),
				utils.ZapInt("bytes", len(data)),
				utils.ZapInt("connected_peers", len(r.Host.Network().Peers())))
		}
		r.bytesPublished.Add(uint64(len(data)))
	}

	// Audit message publication
	if r.secCfg.AuditLogger != nil {
		r.secCfg.AuditLogger.Info("p2p_message_published", map[string]interface{}{
			"topic":   topic,
			"size":    len(data),
			"success": err == nil,
		})
	}

	return err
}

func (r *Router) monitorPeerLatency() {
	interval := r.latencyProbeInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.probePeerLatencies()
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Router) probePeerLatencies() {
	if r.pingService == nil || r.Host == nil {
		return
	}
	peers := r.Host.Network().Peers()
	if len(peers) == 0 {
		return
	}
	timeout := r.latencyProbeTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	for _, pid := range peers {
		if len(r.Host.Network().ConnsToPeer(pid)) == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(r.ctx, timeout)
		resultCh := r.pingService.Ping(ctx, pid)
		select {
		case res, ok := <-resultCh:
			if ok && res.Error == nil && res.RTT > 0 {
				r.storePeerLatency(pid, res.RTT)
			}
		case <-ctx.Done():
		case <-r.ctx.Done():
		}
		cancel()
	}
}

func (r *Router) adjacencyBroadcastLoop() {
	interval := r.adjBroadcastInterval
	if interval <= 0 || r.adjTopic == "" {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Emit an initial snapshot to accelerate convergence.
	r.broadcastAdjacency()
	for {
		select {
		case <-ticker.C:
			r.broadcastAdjacency()
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Router) broadcastAdjacency() {
	if r == nil || r.Host == nil || r.Gossip == nil || r.adjTopic == "" {
		return
	}
	stats := r.GetNetworkStats()
	payload := adjacencyPayload{
		Reporter:   stats.Self.String(),
		ObservedAt: stats.Timestamp.Unix(),
	}
	localEdges, _, _ := r.collectLocalEdges(stats.Timestamp)
	if r.adjMaxEdges > 0 && len(localEdges) > r.adjMaxEdges {
		localEdges = localEdges[:r.adjMaxEdges]
	}
	edges := make([]adjacencyEdgePayload, 0, len(localEdges))
	for _, edge := range localEdges {
		updated := edge.UpdatedAt.Unix()
		if edge.UpdatedAt.IsZero() {
			updated = stats.Timestamp.Unix()
		}
		edges = append(edges, adjacencyEdgePayload{
			Target:     edge.Target.String(),
			Direction:  edge.Direction,
			Status:     edge.Status,
			Confidence: edge.Confidence,
			UpdatedAt:  updated,
		})
	}
	payload.Edges = edges
	peersCopy := append([]RouterPeerStats(nil), stats.Peers...)
	sort.Slice(peersCopy, func(i, j int) bool {
		return peersCopy[i].ID.String() < peersCopy[j].ID.String()
	})
	peerLimit := len(peersCopy)
	if r.adjMaxEdges > 0 && peerLimit > r.adjMaxEdges {
		peerLimit = r.adjMaxEdges
	}
	peersPayload := make([]adjacencyPeerPayload, 0, peerLimit)
	for i := 0; i < peerLimit; i++ {
		ps := peersCopy[i]
		lastSeen := int64(0)
		if !ps.LastSeen.IsZero() {
			lastSeen = ps.LastSeen.Unix()
		}
		status := ps.Status
		if status == "" {
			status = "unknown"
		}
		peersPayload = append(peersPayload, adjacencyPeerPayload{
			Peer:      ps.ID.String(),
			Status:    status,
			RateBps:   ps.RateBps,
			LatencyMs: ps.LatencyMs,
			LastSeen:  lastSeen,
			BytesIn:   ps.BytesIn,
		})
	}
	payload.Peers = peersPayload
	data, err := json.Marshal(payload)
	if err != nil {
		r.log.Warn("adjacency payload marshal failed", utils.ZapError(err))
		return
	}
	if len(payload.Edges) == 0 && len(payload.Peers) == 0 {
		return
	}
	if err := r.Publish(r.adjTopic, data); err != nil {
		r.log.Warn("adjacency publish failed", utils.ZapError(err))
	}
}

func (r *Router) handleAdjacencyMessage(ctx context.Context, from peer.ID, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var payload adjacencyPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("decode adjacency payload: %w", err)
	}
	reporter := from
	if payload.Reporter != "" {
		if pid, err := peer.Decode(payload.Reporter); err == nil {
			reporter = pid
		}
	}
	observed := time.Now()
	if payload.ObservedAt > 0 {
		observed = time.Unix(payload.ObservedAt, 0)
	}
	r.ingestReportedEdges(reporter, payload.Edges, observed)
	r.ingestPeerObservations(reporter, payload.Peers, observed)
	return nil
}

func (r *Router) ingestReportedEdges(reporter peer.ID, edges []adjacencyEdgePayload, observed time.Time) {
	if len(edges) == 0 {
		return
	}
	sanitized := make([]RouterEdge, 0, len(edges))
	for _, edge := range edges {
		targetID, err := peer.Decode(edge.Target)
		if err != nil {
			continue
		}
		updated := observed
		if edge.UpdatedAt > 0 {
			updated = time.Unix(edge.UpdatedAt, 0)
		}
		confidence := edge.Confidence
		if confidence == "" {
			confidence = "reported"
		}
		status := edge.Status
		if status == "" {
			status = "unknown"
		}
		direction := edge.Direction
		if direction == "" {
			direction = "unknown"
		}
		sanitized = append(sanitized, RouterEdge{
			Source:     reporter,
			Target:     targetID,
			Direction:  direction,
			Status:     status,
			Confidence: confidence,
			ReportedBy: reporter,
			UpdatedAt:  updated,
		})
	}
	if len(sanitized) == 0 {
		return
	}
	entry := reportedEdges{Edges: sanitized, Collected: time.Now()}
	r.adjMu.Lock()
	r.reportedAdjacency[reporter] = entry
	r.adjMu.Unlock()
}

func (r *Router) ingestPeerObservations(reporter peer.ID, peers []adjacencyPeerPayload, observed time.Time) {
	if len(peers) == 0 {
		return
	}
	now := time.Now()
	r.adjMu.Lock()
	if r.peerObservations == nil {
		r.peerObservations = make(map[peer.ID]observedPeerStat)
	}
	for _, p := range peers {
		targetID, err := peer.Decode(p.Peer)
		if err != nil {
			continue
		}
		status := p.Status
		if status == "" {
			status = "unknown"
		}
		lastSeen := observed
		if p.LastSeen > 0 {
			lastSeen = time.Unix(p.LastSeen, 0)
		}
		stat := RouterPeerStats{
			ID:        targetID,
			Status:    status,
			RateBps:   p.RateBps,
			LatencyMs: p.LatencyMs,
			LastSeen:  lastSeen,
			BytesIn:   p.BytesIn,
			Labels: map[string]string{
				"reported_by": reporter.String(),
			},
		}
		r.peerObservations[targetID] = observedPeerStat{Stats: stat, Collected: now}
	}
	r.adjMu.Unlock()
}

func (r *Router) collectLocalEdges(sampleTime time.Time) ([]RouterEdge, int, int) {
	if r.Host == nil {
		return nil, 0, 0
	}
	edgeFlags := make(map[peer.ID]struct {
		inbound  bool
		outbound bool
	})
	inboundCount := 0
	outboundCount := 0
	for _, conn := range r.Host.Network().Conns() {
		remote := conn.RemotePeer()
		flags := edgeFlags[remote]
		switch conn.Stat().Direction {
		case network.DirInbound:
			inboundCount++
			flags.inbound = true
		case network.DirOutbound:
			outboundCount++
			flags.outbound = true
		}
		edgeFlags[remote] = flags
	}
	edges := make([]RouterEdge, 0, len(edgeFlags))
	for pid, flags := range edgeFlags {
		direction := "unknown"
		switch {
		case flags.inbound && flags.outbound:
			direction = "bidirectional"
		case flags.inbound:
			direction = "inbound"
		case flags.outbound:
			direction = "outbound"
		}
		edges = append(edges, RouterEdge{
			Source:     r.Host.ID(),
			Target:     pid,
			Direction:  direction,
			Status:     "live",
			Confidence: "observed",
			ReportedBy: r.Host.ID(),
			UpdatedAt:  sampleTime,
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source == edges[j].Source {
			return edges[i].Target.String() < edges[j].Target.String()
		}
		return edges[i].Source.String() < edges[j].Source.String()
	})
	return edges, inboundCount, outboundCount
}

func (r *Router) collectReportedEdges(now time.Time) []RouterEdge {
	r.adjMu.Lock()
	defer r.adjMu.Unlock()
	if len(r.reportedAdjacency) == 0 {
		return nil
	}
	edges := make([]RouterEdge, 0)
	for reporter, entry := range r.reportedAdjacency {
		if r.reportedExpiry > 0 && now.Sub(entry.Collected) > r.reportedExpiry {
			delete(r.reportedAdjacency, reporter)
			continue
		}
		for _, edge := range entry.Edges {
			copy := edge
			if copy.ReportedBy == "" {
				copy.ReportedBy = reporter
			}
			if copy.Confidence == "" {
				copy.Confidence = "reported"
			}
			edges = append(edges, copy)
		}
	}
	return edges
}

func mergeEdgeSets(local, reported []RouterEdge) []RouterEdge {
	if len(reported) == 0 {
		if len(local) == 0 {
			return nil
		}
		out := make([]RouterEdge, len(local))
		copy(out, local)
		return out
	}
	merged := make(map[string]RouterEdge, len(reported)+len(local))
	for _, edge := range reported {
		key := edge.Source.String() + "|" + edge.Target.String()
		merged[key] = edge
	}
	for _, edge := range local {
		key := edge.Source.String() + "|" + edge.Target.String()
		merged[key] = edge
	}
	out := make([]RouterEdge, 0, len(merged))
	for _, edge := range merged {
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source == out[j].Source {
			return out[i].Target.String() < out[j].Target.String()
		}
		return out[i].Source.String() < out[j].Source.String()
	})
	return out
}

func (r *Router) getObservedPeerStats(pid peer.ID, now time.Time) (RouterPeerStats, bool) {
	r.adjMu.Lock()
	defer r.adjMu.Unlock()
	entry, ok := r.peerObservations[pid]
	if !ok {
		return RouterPeerStats{}, false
	}
	if r.observationsExpiry > 0 && now.Sub(entry.Collected) > r.observationsExpiry {
		delete(r.peerObservations, pid)
		return RouterPeerStats{}, false
	}
	stat := entry.Stats
	return stat, true
}

func (r *Router) storePeerLatency(pid peer.ID, rtt time.Duration) {
	r.latencyMu.Lock()
	r.peerLatency[pid] = rtt
	r.latencyMu.Unlock()
	if r.state != nil {
		r.state.RecordLatencySample(pid, rtt)
	}
}

func (r *Router) getPeerLatency(pid peer.ID) (time.Duration, bool) {
	r.latencyMu.RLock()
	defer r.latencyMu.RUnlock()
	rtt, ok := r.peerLatency[pid]
	return rtt, ok
}

func derivePeerStatus(ps PeerState, now time.Time) string {
	if ps.Quarantined {
		return "critical"
	}
	if ps.LastSeen.IsZero() {
		return "unknown"
	}
	elapsed := now.Sub(ps.LastSeen)
	switch {
	case elapsed > 2*time.Minute:
		return "critical"
	case elapsed > 30*time.Second:
		return "warning"
	default:
		return "healthy"
	}
}

func peerIDFromPublicKey(pub []byte) (peer.ID, error) {
	if len(pub) == 0 {
		return "", fmt.Errorf("empty public key")
	}
	hash := sha256.Sum256(pub)
	_, pid, err := fromSeed(hash[:])
	if err != nil {
		return "", err
	}
	return pid, nil
}

func clampDuration(v, min, max time.Duration) time.Duration {
	if v <= 0 {
		return min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// GetNetworkStats returns aggregate transport telemetry for diagnostics.
func (r *Router) GetNetworkStats() RouterStats {
	var out RouterStats
	if r == nil {
		return out
	}

	sampleTime := time.Now()
	out.Timestamp = sampleTime

	if r.Host != nil {
		out.Self = r.Host.ID()
		out.PeerCount = len(r.Host.Network().Peers())
		localEdges, inboundCount, outboundCount := r.collectLocalEdges(sampleTime)
		out.InboundPeers = inboundCount
		out.OutboundPeers = outboundCount
		reportedEdges := r.collectReportedEdges(sampleTime)
		out.Edges = mergeEdgeSets(localEdges, reportedEdges)
	}

	var elapsed time.Duration
	r.throughputMu.Lock()
	if !r.lastSampleTime.IsZero() {
		elapsed = sampleTime.Sub(r.lastSampleTime)
	}

	if r.state != nil {
		snapshot := r.state.Snapshot()
		peers := make([]RouterPeerStats, 0, len(snapshot))
		var latencySum float64
		var latencyCount int
		for pid, ps := range snapshot {
			out.BytesReceived += ps.BytesIn
			peerMetric := RouterPeerStats{
				ID:       pid,
				BytesIn:  ps.BytesIn,
				BytesOut: 0,
				LastSeen: ps.LastSeen,
				Labels:   ps.Labels,
				Status:   derivePeerStatus(ps, sampleTime),
			}
			if ps.LatencyEMA > 0 {
				peerMetric.LatencyMs = ps.LatencyEMA * 1000
				latencySum += ps.LatencyEMA
				latencyCount++
			} else if rtt, ok := r.getPeerLatency(pid); ok && rtt > 0 {
				peerMetric.LatencyMs = rtt.Seconds() * 1000
				latencySum += rtt.Seconds()
				latencyCount++
			}
			if elapsed > 0 {
				if prev, ok := r.peerRateSnapshots[pid]; ok {
					delta := int64(ps.BytesIn) - int64(prev.bytesIn)
					if delta < 0 {
						delta = 0
					}
					peerMetric.RateBps = float64(delta) / elapsed.Seconds()
				}
			}
			r.peerRateSnapshots[pid] = peerRateSample{bytesIn: ps.BytesIn}
			peers = append(peers, peerMetric)
		}
		if latencyCount > 0 {
			out.AvgLatencyMs = (latencySum / float64(latencyCount)) * 1000
		}
		out.Peers = peers
	}

	out.BytesSent = r.bytesPublished.Load()

	if elapsed > 0 {
		inDelta := int64(out.BytesReceived) - int64(r.lastBytesReceived)
		if inDelta < 0 {
			inDelta = 0
		}
		outDelta := int64(out.BytesSent) - int64(r.lastBytesSent)
		if outDelta < 0 {
			outDelta = 0
		}
		if elapsed.Seconds() > 0 {
			out.InboundRateBps = float64(inDelta) / elapsed.Seconds()
			out.OutboundRateBps = float64(outDelta) / elapsed.Seconds()
		}
	}

	r.lastBytesReceived = out.BytesReceived
	r.lastBytesSent = out.BytesSent
	r.lastSampleTime = sampleTime
	r.throughputMu.Unlock()

	r.recordTrend(out)
	out.History = r.trendSnapshot()
	return out
}

// ValidatorPeerStats returns router peer stats keyed by validator ID. Validators without
// observed router metrics are marked offline with zeroed transport data.
func (r *Router) ValidatorPeerStats(validators []types.ValidatorInfo) map[types.ValidatorID]RouterPeerStats {
	result := make(map[types.ValidatorID]RouterPeerStats, len(validators))
	if r == nil || len(validators) == 0 {
		return result
	}

	netStats := r.GetNetworkStats()
	peerIndex := make(map[string]RouterPeerStats, len(netStats.Peers))
	for _, peerStat := range netStats.Peers {
		peerIndex[strings.ToLower(peerStat.ID.String())] = peerStat
	}
	now := time.Now()

	for _, v := range validators {
		var (
			pid peer.ID
			err error
		)
		if strings.TrimSpace(v.PeerID) != "" {
			pid, err = peer.Decode(v.PeerID)
			if err != nil {
				r.log.Warn("invalid configured peer id for validator", utils.ZapError(err))
				continue
			}
		} else {
			pid, err = peerIDFromPublicKey(v.PublicKey)
			if err != nil {
				continue
			}
		}
		peerKey := strings.ToLower(pid.String())
		if stat, ok := peerIndex[peerKey]; ok {
			result[v.ID] = stat
			continue
		}
		if obs, ok := r.getObservedPeerStats(pid, now); ok {
			result[v.ID] = obs
			continue
		}

		result[v.ID] = RouterPeerStats{
			ID:       pid,
			Status:   "offline",
			LastSeen: v.LastSeen,
		}
	}

	return result
}

func (r *Router) recordTrend(sample RouterStats) {
	if r == nil {
		return
	}
	entry := RouterTrendSample{
		Timestamp:     sample.Timestamp,
		PeerCount:     sample.PeerCount,
		InboundPeers:  sample.InboundPeers,
		OutboundPeers: sample.OutboundPeers,
		AvgLatencyMs:  sample.AvgLatencyMs,
		BytesReceived: sample.BytesReceived,
		BytesSent:     sample.BytesSent,
	}
	r.trendMu.Lock()
	defer r.trendMu.Unlock()
	r.trendSamples = append(r.trendSamples, entry)
	if r.trendWindow > 0 {
		cutoff := sample.Timestamp.Add(-r.trendWindow)
		idx := 0
		for idx < len(r.trendSamples) && r.trendSamples[idx].Timestamp.Before(cutoff) {
			idx++
		}
		if idx > 0 {
			r.trendSamples = append([]RouterTrendSample(nil), r.trendSamples[idx:]...)
		}
	}
	if r.trendMaxSamples > 0 && len(r.trendSamples) > r.trendMaxSamples {
		r.trendSamples = append([]RouterTrendSample(nil), r.trendSamples[len(r.trendSamples)-r.trendMaxSamples:]...)
	}
}

func (r *Router) trendSnapshot() []RouterTrendSample {
	r.trendMu.RLock()
	defer r.trendMu.RUnlock()
	if len(r.trendSamples) == 0 {
		return nil
	}
	out := make([]RouterTrendSample, len(r.trendSamples))
	copy(out, r.trendSamples)
	return out
}

// GetConnectedPeerCount returns the number of connected, non-quarantined peers
// Used for quorum checks before proposing blocks
func (r *Router) GetConnectedPeerCount() int {
	if r.state == nil {
		return 0
	}
	return r.state.GetConnectedPeerCount()
}

// GetActivePeerCount returns the number of recently active peers
// Used to detect connected but unresponsive peers
func (r *Router) GetActivePeerCount(since time.Duration) int {
	if r.state == nil {
		return 0
	}
	r.refreshConnectionActivity()
	return r.state.GetActivePeerCount(since)
}

func (r *Router) refreshConnectionActivity() {
	if r.Host == nil || r.state == nil {
		return
	}
	now := time.Now()
	peers := r.Host.Network().Peers()
	if len(peers) == 0 {
		return
	}
	for _, pid := range peers {
		if len(r.Host.Network().ConnsToPeer(pid)) == 0 {
			continue
		}
		r.state.TouchPeer(pid, now)
	}
}

// GetPeersSeenSinceStartup returns the count of peers observed since startup.
func (r *Router) GetPeersSeenSinceStartup() int {
	if r.state == nil {
		return 0
	}
	return r.state.GetPeersSeenSinceStartup()
}

// PeerHash returns a deterministic fingerprint over the currently known peers (including self).
func (r *Router) PeerHash() ([32]byte, error) {
	var out [32]byte
	if r.Host == nil {
		return out, fmt.Errorf("router host not initialized")
	}
	if r.state == nil {
		h := sha256.Sum256([]byte(r.Host.ID().String()))
		return h, nil
	}
	snapshot := r.state.Snapshot()
	ids := make([]string, 0, len(snapshot)+1)
	ids = append(ids, r.Host.ID().String())
	for pid := range snapshot {
		ids = append(ids, pid.String())
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		_, _ = h.Write([]byte(id))
		_, _ = h.Write([]byte{0})
	}
	copy(out[:], h.Sum(nil))
	return out, nil
}

// GetPeerCount returns the total number of tracked peers
func (r *Router) GetPeerCount() int {
	if r.state == nil {
		return 0
	}
	return r.state.GetPeerCount()
}

// Close shuts everything down.
func (r *Router) Close() error {
	r.cancel()
	if r.DHT != nil {
		_ = r.DHT.Close()
	}
	if r.Host != nil {
		return r.Host.Close()
	}
	return nil
}

// --- internal ---

func (r *Router) consume(topic string, sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(r.ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				r.log.Warn("topic consumer stopped", utils.ZapString("topic", topic), utils.ZapError(err))
			}
			return
		}
		if msg.ReceivedFrom == "" || len(msg.Data) == 0 {
			continue
		}
		from := msg.ReceivedFrom
		r.log.Info("got message",
			utils.ZapString("topic", topic),
			utils.ZapString("from", from.String()),
			utils.ZapInt("bytes", len(msg.Data)))
		// Update peer activity in state
		if r.state != nil {
			r.state.OnMessage(topic, from, len(msg.Data))
		}
		// Dispatch to handlers
		r.mu.RLock()
		hs := append([]Handler(nil), r.Handlers[topic]...)
		r.mu.RUnlock()
		for _, h := range hs {
			// Acquire semaphore before spawning goroutine
			select {
			case r.handlerSem <- struct{}{}:
				go func(h Handler) {
					defer func() { <-r.handlerSem }() // release

					// Panic recovery to prevent handler crashes
					defer func() {
						if rec := recover(); rec != nil {
							r.log.Error("handler panic",
								utils.ZapString("topic", topic),
								utils.ZapAny("panic", rec))
						}
					}()

					_ = h(r.ctx, from, msg.Data)
				}(h)
			case <-r.ctx.Done():
				return
			}
		}
	}
}

func (r *Router) dialBootstrapPeers() error {
	// Priority: explicit opts > config.MeshConfig.BootstrapNodes > config manager
	var addrs []string
	if len(r.opts.BootstrapAddrs) > 0 {
		addrs = r.opts.BootstrapAddrs
	} else if r.nodeCfg != nil && r.nodeCfg.MeshConfig != nil {
		addrs = r.nodeCfg.MeshConfig.BootstrapNodes
	} else {
		addrs = r.configMgr.GetStringSlice("P2P_BOOTSTRAP_PEERS", []string{})
	}

	if len(addrs) == 0 {
		return nil
	}

	var errs []string
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}

		// Parse multiaddr
		if strings.HasPrefix(addr, "/") {
			maddr, err := multiaddr.NewMultiaddr(addr)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: invalid multiaddr: %v", addr, err))
				continue
			}

			// Extract peer info and connect
			peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: failed to extract peer info: %v", addr, err))
				continue
			}

			// Attempt connection
			ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
			err = r.Host.Connect(ctx, *peerInfo)
			cancel()

			if err != nil {
				r.log.Debug("bootstrap connection failed",
					utils.ZapString("peer", peerInfo.ID.String()),
					utils.ZapError(err))
			} else {
				r.log.Info("bootstrap connection successful",
					utils.ZapString("peer", peerInfo.ID.String()))
			}
			continue
		}

		// Handle host:port format
		host, port, err := utils.SplitHostPortLoose(addr)
		if err != nil {
			host = addr
			port = ""
		}
		if net.ParseIP(host) == nil {
			// DNS resolution
			ips, err := net.LookupHost(host)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", host, err))
				continue
			}
			r.log.Info("resolved bootstrap peer",
				utils.ZapString("host", host),
				utils.ZapAny("ips", ips),
				utils.ZapString("port", port))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("bootstrap resolution issues: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (r *Router) discoveryLoop(rendezvous string) {
	interval := r.configMgr.GetDuration("P2P_DISCOVERY_INTERVAL", 15*time.Second)
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-t.C:
			peerCh, err := r.Discovery.FindPeers(r.ctx, rendezvous)
			if err != nil {
				r.log.Warn("discovery error", utils.ZapError(err))
				continue
			}
			for p := range peerCh {
				if p.ID == "" || p.ID == r.Host.ID() {
					continue
				}
				// skip quarantined
				if r.state != nil && r.state.IsQuarantined(p.ID) {
					continue
				}
				// best-effort dial
				_ = r.Host.Connect(r.ctx, p)
			}
		}
	}
}

// --- identity / helpers ---

func deriveIdentity(secCfg *config.SecurityConfig, configMgr *utils.ConfigManager) (crypto.PrivKey, peer.ID, error) {
	// ENV seed (hex 32 bytes)
	if seedHex := strings.TrimSpace(configMgr.GetString("P2P_ID_SEED", "")); seedHex != "" {
		seed, err := hex.DecodeString(seedHex)
		if err == nil && len(seed) >= 32 {
			return fromSeed(seed[:32])
		}
	}

	// Try to derive from existing crypto service
	if secCfg.CryptoService != nil {
		pubKey, err := secCfg.CryptoService.GetPublicKey()
		if err == nil && len(pubKey) > 0 {
			h := sha256.Sum256(pubKey)
			return fromSeed(h[:])
		}
	}

	// Production must not use random IDs
	env := configMgr.GetString("ENVIRONMENT", "production")
	if env == "production" || env == "staging" {
		return nil, "", fmt.Errorf("no P2P identity seed provided in production")
	}

	// Development only: generate random key (will break across restarts)
	// GenerateEd25519Key returns (PrivKey, PubKey, error)
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	pid, err := peer.IDFromPrivateKey(priv)
	return priv, pid, err
}

func fromSeed(seed []byte) (crypto.PrivKey, peer.ID, error) {
	// NewKeyFromSeed returns a 64-byte Ed25519 private key
	std := ed25519.NewKeyFromSeed(seed)

	// UnmarshalEd25519PrivateKey expects the 64-byte private key
	libPriv, err := crypto.UnmarshalEd25519PrivateKey([]byte(std))
	if err != nil {
		return nil, "", err
	}

	pid, err := peer.IDFromPrivateKey(libPriv)
	return libPriv, pid, err
}

// netNotifiee relays connection events into State.
type netNotifiee struct{ r *Router }

func (n *netNotifiee) Listen(net network.Network, addr multiaddr.Multiaddr)      {}
func (n *netNotifiee) ListenClose(net network.Network, addr multiaddr.Multiaddr) {}
func (n *netNotifiee) Connected(net network.Network, c network.Conn) {
	if n.r == nil || n.r.state == nil {
		return
	}
	labels := map[string]string{
		"direction": c.Stat().Direction.String(),
		"local":     c.LocalMultiaddr().String(),
		"remote":    c.RemoteMultiaddr().String(),
	}
	n.r.state.OnConnect(c.RemotePeer(), labels)
}
func (n *netNotifiee) Disconnected(net network.Network, c network.Conn) {
	if n.r == nil || n.r.state == nil {
		return
	}
	n.r.state.OnDisconnect(c.RemotePeer())
}
func (n *netNotifiee) OpenedStream(network.Network, network.Stream) {}
func (n *netNotifiee) ClosedStream(network.Network, network.Stream) {}

// connGater enforces IP allowlists, trusted peers, and State quarantine at the transport layer.
type connGater struct {
	allowed []net.IPNet
	trusted map[peer.ID]bool
	state   *State
	log     *utils.Logger
}

func (g *connGater) allowIP(addr multiaddr.Multiaddr) bool {
	if len(g.allowed) == 0 {
		return true // no restriction
	}
	var ip net.IP

	// Use multiaddr's built-in IP extraction
	multiaddr.ForEach(addr, func(c multiaddr.Component) bool {
		switch c.Protocol().Code {
		case multiaddr.P_IP4, multiaddr.P_IP6:
			ip = net.ParseIP(c.Value())
			return false // stop iteration
		}
		return true // continue
	})

	if ip == nil {
		if g.log != nil {
			g.log.Warn("could not extract IP from multiaddr",
				utils.ZapString("addr", addr.String()))
		}
		return false
	}
	for _, n := range g.allowed {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (g *connGater) InterceptAddrDial(p peer.ID, addr multiaddr.Multiaddr) (allow bool) {
	return g.allowIP(addr)
}

func (g *connGater) InterceptPeerDial(p peer.ID) (allow bool) {
	if g.state != nil && g.state.IsQuarantined(p) && !g.trusted[p] {
		if g.log != nil {
			g.log.Warn("gater: block peer dial (quarantined)", utils.ZapString("peer", p.String()))
		}
		return false
	}
	return true
}

func (g *connGater) InterceptAccept(addr network.ConnMultiaddrs) (allow bool) {
	return g.allowIP(addr.RemoteMultiaddr())
}

func (g *connGater) InterceptSecured(dir network.Direction, p peer.ID, addr network.ConnMultiaddrs) (allow bool) {
	if !g.allowIP(addr.RemoteMultiaddr()) {
		return false
	}
	if g.state != nil && g.state.IsQuarantined(p) && !g.trusted[p] {
		return false
	}
	return true
}

func (g *connGater) InterceptUpgraded(network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}

// --- misc helpers ---

func hasIPv6() bool {
	ifcs, _ := net.Interfaces()
	for _, ifc := range ifcs {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To16() != nil && ipnet.IP.To4() == nil {
				return true
			}
		}
	}
	return false
}

func parsePeerIDs(list []string) map[peer.ID]bool {
	m := make(map[peer.ID]bool, len(list))
	for _, s := range list {
		if id, err := peer.Decode(strings.TrimSpace(s)); err == nil {
			m[id] = true
		}
	}
	return m
}

// topic helpers
func containsTopic(list []string, target string) bool {
	for _, s := range list {
		if strings.TrimSpace(s) == target {
			return true
		}
	}
	return false
}

func dedupeTopics(list []string) []string {
	seen := make(map[string]struct{}, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// mdnsNotifee handles mDNS peer discovery notifications
type mdnsNotifee struct {
	h   host.Host
	log *utils.Logger
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.h.ID() {
		return // skip self
	}
	n.log.Info("mDNS peer discovered", utils.ZapString("peer_id", pi.ID.String()))
	if err := n.h.Connect(context.Background(), pi); err != nil {
		n.log.Warn("failed to connect to mDNS peer", utils.ZapString("peer_id", pi.ID.String()), utils.ZapError(err))
	} else {
		n.log.Info("connected to mDNS peer", utils.ZapString("peer_id", pi.ID.String()))
		// Protect validator peer connections from connection manager pruning
		n.h.ConnManager().TagPeer(pi.ID, "validator", 1000)
		n.h.ConnManager().Protect(pi.ID, "validator-peer")
	}
}
