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
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
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
	noise "github.com/libp2p/go-libp2p/p2p/security/noise"
	tlsp2p "github.com/libp2p/go-libp2p/p2p/security/tls"
	multiaddr "github.com/multiformats/go-multiaddr"

	// project-local packages
	"backend/pkg/config"
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
		ctx:       ctx,
		cancel:    cancel,
		log:       log,
		Host:      host,
		DHT:       ipfsDHT,
		Gossip:    ps,
		Topics:    map[string]*pubsub.Topic{},
		Subs:      map[string]*pubsub.Subscription{},
		Handlers:  map[string][]Handler{},
		Discovery: rd,
		nodeCfg:   nodeCfg,
		secCfg:    secCfg,
		configMgr: configMgr,
		state:     st,
		opts:      opts,
	}

	// Create semaphore for bounded handler concurrency
	maxHandlers := configMgr.GetIntRange("P2P_MAX_HANDLER_GOROUTINES", 200, 10, 1000)
	r.handlerSem = make(chan struct{}, maxHandlers)

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
	return r.state.GetActivePeerCount(since)
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
