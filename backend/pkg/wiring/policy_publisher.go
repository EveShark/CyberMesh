package wiring

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/control/policyoutbox"
	"backend/pkg/control/policytrace"
	"backend/pkg/ingest/kafka"
	"backend/pkg/utils"
	pb "backend/proto"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type policyProducer interface {
	PublishPolicy(ctx context.Context, evt *pb.PolicyUpdateEvent) error
	PublishPolicyWithAck(ctx context.Context, evt *pb.PolicyUpdateEvent) (int32, int64, error)
	PublishPolicyWithRoutingKey(ctx context.Context, evt *pb.PolicyUpdateEvent, routingKey string) (int32, int64, error)
	PublishDLQ(ctx context.Context, topic string, key sarama.Encoder, payload []byte, headers []sarama.RecordHeader) (int32, int64, error)
}

type policyPublisher struct {
	producer            policyProducer
	logger              *utils.Logger
	audit               *utils.AuditLogger
	enabled             bool
	topic               string
	gatewayNS           string
	maxTTL              time.Duration
	maxCIDRPrefix       int
	maxPerBlock         int
	blockDuration       time.Duration
	rateLimiter         *policyRateLimiter
	dlqTopic            string
	dlqEnabled          bool
	guardrailMu         sync.Mutex
	guardrailHits       map[string]uint64
	dedupeMu            sync.Mutex
	dedupeSuppressed    map[string]uint64
	publishMu           sync.Mutex
	publishedAt         map[string]time.Time
	dedupeWindow        time.Duration
	maxCacheSize        int
	scopeRouting        bool
	clusterShardingMode string
	clusterShardBuckets int
	clusterShardLanes   int
	trace               *policytrace.Collector
	dlqSuccesses        atomic.Uint64
	dlqFailures         atomic.Uint64
	commandStrict       bool
}

const minIPv6Prefix = 64

var supportedPolicyActions = map[string]struct{}{
	"drop":            {},
	"reject":          {},
	"remove":          {},
	"force_reauth":    {},
	"disable_export":  {},
	"freeze_user":     {},
	"freeze_tenant":   {},
	"throttle_action": {},
	"disable_ai_api_for_scope": {},
	"rate_limit":      {},
}

type policyDLQRecord struct {
	Timestamp      time.Time `json:"timestamp"`
	Reason         string    `json:"reason"`
	PolicyID       string    `json:"policy_id,omitempty"`
	Height         uint64    `json:"height,omitempty"`
	Error          string    `json:"error,omitempty"`
	PayloadBase64  string    `json:"payload_base64,omitempty"`
	ProducerID     string    `json:"producer_id,omitempty"`
	GuardrailStage string    `json:"guardrail_stage,omitempty"`
}

func newPolicyPublisher(producer *kafka.Producer, cfgMgr *utils.ConfigManager, topic string, logger *utils.Logger, audit *utils.AuditLogger, trace *policytrace.Collector) (*policyPublisher, error) {
	if producer == nil || topic == "" {
		return nil, nil
	}
	if cfgMgr == nil {
		return nil, fmt.Errorf("policy publisher: config manager required")
	}

	enabled := cfgMgr.GetBool("POLICY_ENFORCEMENT_ENABLED", true)
	maxTTL := cfgMgr.GetDuration("POLICY_TTL_MAX", time.Hour)
	if maxTTL <= 0 {
		maxTTL = time.Hour
	}
	maxCIDR := cfgMgr.GetInt("POLICY_CIDR_MAX_PREFIX", 24)
	if maxCIDR <= 0 {
		maxCIDR = 24
	}
	maxPerBlock := cfgMgr.GetInt("POLICY_TX_MAX_PER_BLOCK", 10)
	maxPerMinute := cfgMgr.GetInt("POLICY_TX_MAX_PER_MIN", 30)
	blockDuration := cfgMgr.GetDuration("CONSENSUS_BLOCK_TIMEOUT", 5*time.Second)
	if blockDuration <= 0 {
		blockDuration = 5 * time.Second
	}
	dedupeWindow := cfgMgr.GetDuration("CONTROL_POLICY_PUBLISH_DEDUP_WINDOW", 10*time.Minute)
	if dedupeWindow < 0 {
		dedupeWindow = 0
	}
	maxCacheSize := cfgMgr.GetInt("CONTROL_POLICY_PUBLISH_DEDUP_CACHE", 10000)
	if maxCacheSize <= 0 {
		maxCacheSize = 10000
	}
	scopeRouting := cfgMgr.GetBool("CONTROL_POLICY_SCOPE_ROUTING_ENABLED", false)
	commandStrict := cfgMgr.GetBool("CONTROL_POLICY_ENFORCEMENT_CONTRACT_STRICT", false)
	rawClusterShardingMode := strings.TrimSpace(cfgMgr.GetString("CONTROL_POLICY_CLUSTER_SHARDING_MODE", policyoutbox.ClusterShardingOff))
	normalizedRouting, unknownMode := policyoutbox.NormalizeRoutingOptions(policyoutbox.RoutingOptions{
		ClusterShardingMode: rawClusterShardingMode,
		ClusterShardBuckets: cfgMgr.GetInt("CONTROL_POLICY_CLUSTER_SHARD_BUCKETS", 1),
		ClusterShardLanes:   cfgMgr.GetInt("CONTROL_POLICY_CLUSTER_SHARD_LANES", 1),
	})
	clusterShardingMode := normalizedRouting.ClusterShardingMode
	clusterShardBuckets := normalizedRouting.ClusterShardBuckets
	clusterShardLanes := normalizedRouting.ClusterShardLanes
	if unknownMode {
		if logger != nil {
			logger.Warn("Unsupported CONTROL_POLICY_CLUSTER_SHARDING_MODE; disabling cluster sharding",
				utils.ZapString("mode", rawClusterShardingMode))
		}
		if audit != nil {
			_ = audit.Warn("policy_cluster_sharding_mode_invalid", map[string]interface{}{
				"component": "publisher",
				"mode":      rawClusterShardingMode,
			})
		}
	}

	dlqTopic := strings.TrimSpace(cfgMgr.GetString("CONTROL_POLICY_DLQ_TOPIC", ""))
	dlqEnabled := cfgMgr.GetBool("POLICY_DLQ_ENABLED", dlqTopic != "")
	if dlqEnabled && dlqTopic == "" {
		dlqEnabled = false
		if logger != nil {
			logger.Warn("Policy DLQ requested but CONTROL_POLICY_DLQ_TOPIC is empty; disabling DLQ")
		}
	}

	pp := &policyPublisher{
		producer:            producer,
		logger:              logger,
		audit:               audit,
		enabled:             enabled,
		topic:               topic,
		maxTTL:              maxTTL,
		maxCIDRPrefix:       maxCIDR,
		maxPerBlock:         maxPerBlock,
		blockDuration:       blockDuration,
		rateLimiter:         newPolicyRateLimiter(maxPerMinute),
		dlqTopic:            dlqTopic,
		dlqEnabled:          dlqEnabled,
		guardrailHits:       make(map[string]uint64),
		dedupeSuppressed:    make(map[string]uint64),
		publishedAt:         make(map[string]time.Time),
		dedupeWindow:        dedupeWindow,
		maxCacheSize:        maxCacheSize,
		scopeRouting:        scopeRouting,
		clusterShardingMode: clusterShardingMode,
		clusterShardBuckets: clusterShardBuckets,
		clusterShardLanes:   clusterShardLanes,
		trace:               trace,
		commandStrict:       commandStrict,
	}

	// Optional: when set, any policy payload that targets this namespace is treated as a
	// gateway enforcement policy and must adhere to stricter semantics (egress-only).
	// We use the namespace selector (target.selectors.namespace) since the policy payload
	// schema already supports selectors and enforcement-agent canonicalizes namespace there.
	pp.gatewayNS = strings.ToLower(strings.TrimSpace(cfgMgr.GetString("POLICY_GATEWAY_NAMESPACE", "")))

	if !enabled && logger != nil {
		logger.Info("Policy publishing disabled via POLICY_ENFORCEMENT_ENABLED=false")
	}
	if logger != nil {
		logger.Info("Policy command contract mode configured",
			utils.ZapBool("strict", commandStrict))
	}

	return pp, nil
}

func (p *policyPublisher) Publish(ctx context.Context, height uint64, ts int64, _ int, payloads [][]byte) {
	if p == nil || !p.enabled {
		return
	}
	if len(payloads) == 0 {
		return
	}

	seen := make(map[string]struct{})

	if p.maxPerBlock > 0 && len(payloads) > p.maxPerBlock {
		p.guardrailReject(ctx, "", height, "policy_tx_max_per_block", nil, nil)
		return
	}

	blockTime := time.Unix(ts, 0)
	if blockTime.IsZero() {
		blockTime = time.Unix(ts, 0)
	}

	if !p.rateLimiter.Allow(blockTime, len(payloads)) {
		p.guardrailReject(ctx, "", height, "policy_tx_max_per_min", nil, nil)
		return
	}

	for _, raw := range payloads {
		dedupeToken := ""
		if key := buildPolicyDedupKey(raw); key != "" {
			dedupeToken = key
			if _, exists := seen[key]; exists {
				p.recordDedupeSuppressed("same_batch")
				if p.logger != nil {
					p.logger.DebugContext(ctx, "Duplicate policy payload skipped",
						utils.ZapString("dedup_key", key))
				}
				continue
			}
			seen[key] = struct{}{}
		}

		evt, policyID, reason, err := p.prepareEvent(height, blockTime, raw)
		if reason != "" || err != nil {
			p.guardrailReject(ctx, policyID, height, reason, err, raw)
			continue
		}
		if dedupeToken == "" {
			dedupeToken = policyID
		}
		if p.alreadyPublishedRecently(dedupeToken, blockTime) {
			p.recordDedupeSuppressed("within_window")
			if p.logger != nil {
				p.logger.InfoContext(ctx, "Skipping duplicate policy publish within dedupe window",
					utils.ZapString("policy_id", policyID),
					utils.ZapDuration("dedupe_window", p.dedupeWindow))
			}
			continue
		}

		if err := p.producer.PublishPolicy(ctx, evt); err != nil {
			if p.logger != nil {
				p.logger.ErrorContext(ctx, "Failed to publish policy",
					utils.ZapError(err),
					utils.ZapString("policy_id", evt.GetPolicyId()),
					utils.ZapUint64("height", height))
			}
			if p.audit != nil {
				_ = p.audit.Error("policy_publish_failed", map[string]interface{}{
					"policy_id": evt.GetPolicyId(),
					"height":    height,
					"error":     err.Error(),
				})
			}
			p.guardrailReject(ctx, evt.GetPolicyId(), height, "policy_publish_failed", err, raw)
			continue
		}

		if p.audit != nil {
			_ = p.audit.Info("policy_published", map[string]interface{}{
				"policy_id":         evt.GetPolicyId(),
				"height":            height,
				"expiration_height": evt.GetExpirationHeight(),
			})
		}
		p.markPublished(dedupeToken, blockTime)
	}
}

// PublishFromOutbox publishes one durable outbox payload and returns policy id and Kafka coordinates.
// It bypasses time-window dedupe because outbox rows are already idempotent at write-time.
func (p *policyPublisher) PublishFromOutbox(ctx context.Context, height uint64, ts int64, raw []byte) (string, int32, int64, error) {
	if p == nil || !p.enabled {
		return "", 0, 0, fmt.Errorf("policy publisher disabled")
	}
	blockTime := time.Unix(ts, 0)
	if blockTime.IsZero() {
		blockTime = time.Now().UTC()
	}
	evt, policyID, reason, err := p.prepareEvent(height, blockTime, raw)
	if reason != "" || err != nil {
		p.guardrailReject(ctx, policyID, height, reason, err, raw)
		if err == nil {
			err = fmt.Errorf("%s", reason)
		}
		return policyID, 0, 0, err
	}

	routingKey := ""
	if p.scopeRouting {
		_, routingKey, _ = policyoutbox.DeriveScopeIdentifierWithOptions(raw, policyoutbox.RoutingOptions{
			ClusterShardingMode: p.clusterShardingMode,
			ClusterShardBuckets: p.clusterShardBuckets,
			ClusterShardLanes:   p.clusterShardLanes,
		})
	}

	partition, offset, err := p.producer.PublishPolicyWithRoutingKey(ctx, evt, routingKey)
	if err != nil {
		p.guardrailReject(ctx, policyID, height, "policy_publish_failed", err, raw)
		return policyID, partition, offset, err
	}

	if p.audit != nil {
		_ = p.audit.Info("policy_published_outbox", map[string]interface{}{
			"policy_id": policyID,
			"height":    height,
			"partition": partition,
			"offset":    offset,
		})
	}
	return policyID, partition, offset, nil
}

func (p *policyPublisher) alreadyPublishedRecently(key string, now time.Time) bool {
	if p == nil || p.dedupeWindow <= 0 || key == "" {
		return false
	}
	p.publishMu.Lock()
	defer p.publishMu.Unlock()
	p.prunePublishedLocked(now)
	last, ok := p.publishedAt[key]
	if !ok {
		return false
	}
	return now.Sub(last) <= p.dedupeWindow
}

func (p *policyPublisher) markPublished(key string, now time.Time) {
	if p == nil || p.dedupeWindow <= 0 || key == "" {
		return
	}
	p.publishMu.Lock()
	defer p.publishMu.Unlock()
	p.prunePublishedLocked(now)
	p.publishedAt[key] = now
}

func (p *policyPublisher) prunePublishedLocked(now time.Time) {
	if p == nil {
		return
	}
	// Time-window eviction first.
	if p.dedupeWindow > 0 {
		cutoff := now.Add(-p.dedupeWindow)
		for k, ts := range p.publishedAt {
			if ts.Before(cutoff) {
				delete(p.publishedAt, k)
			}
		}
	}
	// Size cap to prevent unbounded growth.
	if p.maxCacheSize > 0 && len(p.publishedAt) > p.maxCacheSize {
		excess := len(p.publishedAt) - p.maxCacheSize
		for k := range p.publishedAt {
			delete(p.publishedAt, k)
			excess--
			if excess <= 0 {
				break
			}
		}
	}
}

func buildPolicyDedupKey(raw []byte) string {
	var params policyParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return ""
	}
	identity := extractPolicyDedupIdentity(raw)

	builder := strings.Builder{}
	builder.WriteString(strings.TrimSpace(params.PolicyID))
	builder.WriteString("|")
	builder.WriteString(strings.ToLower(strings.TrimSpace(params.RuleType)))
	builder.WriteString("|")
	builder.WriteString(strings.ToLower(strings.TrimSpace(params.ControlAction)))
	builder.WriteString("|")
	builder.WriteString(strings.ToLower(strings.TrimSpace(params.Action)))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(params.RollbackPolicyID))
	builder.WriteString("|")

	scope := strings.ToLower(strings.TrimSpace(params.Target.Scope))
	direction := strings.ToLower(strings.TrimSpace(params.Target.Direction))
	builder.WriteString(scope)
	builder.WriteString("|")
	builder.WriteString(direction)
	builder.WriteString("|")

	ips := append([]string(nil), params.Target.IPs...)
	sort.Strings(ips)
	builder.WriteString(strings.Join(ips, ","))
	builder.WriteString("|")

	cidrs := append([]string(nil), params.Target.CIDRs...)
	sort.Strings(cidrs)
	builder.WriteString(strings.Join(cidrs, ","))
	builder.WriteString("|")

	ttl := 0
	if params.Guardrails.TTLSeconds != nil {
		ttl = *params.Guardrails.TTLSeconds
	}
	builder.WriteString(fmt.Sprintf("%d|", ttl))

	allowEntries := params.Guardrails.Allowlist.Entries()
	if allowEntries != nil {
		allow := append([]string(nil), allowEntries...)
		sort.Strings(allow)
		builder.WriteString(strings.Join(allow, ","))
	}
	builder.WriteString("|")

	if len(params.Target.Selectors) > 0 {
		if selBytes, err := json.Marshal(params.Target.Selectors); err == nil {
			builder.Write(selBytes)
		}
	}
	builder.WriteString("|")
	builder.WriteString(identity.CommandID)
	builder.WriteString("|")
	builder.WriteString(identity.RequestID)
	builder.WriteString("|")
	builder.WriteString(identity.WorkflowID)
	builder.WriteString("|")
	builder.WriteString(identity.IdempotencyKey)
	builder.WriteString("|")
	builder.WriteString(identity.TraceID)

	return builder.String()
}

type policyDedupIdentity struct {
	CommandID      string
	RequestID      string
	WorkflowID     string
	IdempotencyKey string
	TraceID        string
}

func extractPolicyDedupIdentity(raw []byte) policyDedupIdentity {
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return policyDedupIdentity{}
	}
	getNested := func(parent map[string]interface{}, key string) string {
		if parent == nil {
			return ""
		}
		value, ok := parent[key]
		if !ok {
			return ""
		}
		s, ok := value.(string)
		if !ok {
			return ""
		}
		return strings.TrimSpace(s)
	}
	getMap := func(key string) map[string]interface{} {
		if root == nil {
			return nil
		}
		child, _ := root[key].(map[string]interface{})
		return child
	}
	metadata := getMap("metadata")
	trace := getMap("trace")
	params := getMap("params")
	return policyDedupIdentity{
		CommandID:      firstNonEmpty(getNested(root, "command_id"), getNested(metadata, "command_id"), getNested(params, "command_id")),
		RequestID:      firstNonEmpty(getNested(root, "request_id"), getNested(metadata, "request_id"), getNested(params, "request_id")),
		WorkflowID:     firstNonEmpty(getNested(root, "workflow_id"), getNested(metadata, "workflow_id"), getNested(params, "workflow_id")),
		IdempotencyKey: firstNonEmpty(getNested(root, "idempotency_key"), getNested(metadata, "idempotency_key"), getNested(params, "idempotency_key")),
		TraceID:        firstNonEmpty(getNested(root, "trace_id"), getNested(trace, "trace_id"), getNested(trace, "id"), getNested(metadata, "trace_id"), getNested(params, "trace_id")),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (p *policyPublisher) prepareEvent(height uint64, blockTime time.Time, raw []byte) (*pb.PolicyUpdateEvent, string, string, error) {
	if len(raw) == 0 {
		return nil, "", "empty_policy_payload", nil
	}

	var payload policyParams
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, "", "invalid_policy_payload", err
	}

	policyID := strings.TrimSpace(payload.PolicyID)
	if policyID == "" {
		return nil, "", "policy_id_missing", nil
	}
	if _, err := uuid.Parse(policyID); err != nil {
		return nil, policyID, "policy_id_invalid", err
	}
	rollbackPolicyID := strings.TrimSpace(payload.RollbackPolicyID)
	if rollbackPolicyID != "" {
		if _, err := uuid.Parse(rollbackPolicyID); err != nil {
			return nil, policyID, "rollback_policy_id_invalid", err
		}
	}

	if payload.SchemaVersion <= 0 {
		return nil, policyID, "schema_version_invalid", nil
	}

	ruleType := strings.TrimSpace(payload.RuleType)
	action := strings.TrimSpace(payload.Action)
	if ruleType != "block" {
		return nil, policyID, "unsupported_rule_type", nil
	}
	if action == "" {
		return nil, policyID, "action_missing", nil
	}
	normalizedAction := strings.ToLower(action)
	if _, ok := supportedPolicyActions[normalizedAction]; !ok {
		return nil, policyID, "action_invalid", fmt.Errorf("action: %s", payload.Action)
	}
	action = normalizedAction

	controlAction := "add"
	if action == "remove" {
		controlAction = "remove"
	}
	if rawControlAction := strings.ToLower(strings.TrimSpace(payload.ControlAction)); rawControlAction != "" {
		switch rawControlAction {
		case "add", "remove":
			if rawControlAction != controlAction {
				return nil, policyID, "control_action_invalid", fmt.Errorf("control_action: %s", payload.ControlAction)
			}
		case "approve", "reject":
			if action == "remove" {
				return nil, policyID, "control_action_invalid", fmt.Errorf("control_action: %s", payload.ControlAction)
			}
			controlAction = rawControlAction
		default:
			return nil, policyID, "control_action_invalid", fmt.Errorf("control_action: %s", payload.ControlAction)
		}
	}

	contract, cErr := buildCommandContract(raw, payload, policyID, controlAction, action, ruleType, blockTime)
	if cErr != nil {
		return nil, policyID, "enforcement_contract_build_failed", cErr
	}
	if vErr := validateCommandContract(contract, commandContractOptions{Strict: p.commandStrict}); vErr != nil {
		return nil, policyID, "enforcement_contract_invalid", vErr
	}
	normalizedRaw, nErr := enrichPolicyPayloadWithContract(raw, contract)
	if nErr != nil {
		return nil, policyID, "enforcement_contract_enrich_failed", nErr
	}

	if action == "remove" {
		hash := sha256.Sum256(normalizedRaw)
		evt := &pb.PolicyUpdateEvent{
			PolicyId:         policyID,
			Action:           controlAction,
			RuleType:         ruleType,
			RuleData:         normalizedRaw,
			RuleHash:         hash[:],
			RequiresAck:      payload.Guardrails.RequiresAck,
			RollbackPolicyId: rollbackPolicyID,
			Timestamp:        blockTime.Unix(),
			EffectiveHeight:  int64(height),
			ExpirationHeight: 0,
		}

		return evt, policyID, "", nil
	}

	direction := strings.TrimSpace(payload.Target.Direction)
	if direction != "" {
		direction = strings.ToLower(direction)
		if direction != "ingress" && direction != "egress" {
			return nil, policyID, "direction_invalid", fmt.Errorf("direction: %s", payload.Target.Direction)
		}
	}

	scope := strings.TrimSpace(payload.Target.Scope)
	if scope != "" {
		scope = strings.ToLower(scope)
		if scope != "cluster" && scope != "namespace" && scope != "node" && scope != "tenant" && scope != "region" {
			return nil, policyID, "scope_invalid", fmt.Errorf("scope: %s", payload.Target.Scope)
		}
	}

	if requiresDirectionalTarget(action) && direction == "" {
		return nil, policyID, "direction_required_for_action", fmt.Errorf("action: %s", action)
	}

	// Gateway enforcement profile: if this payload targets the configured gateway namespace,
	// enforce egress-only semantics for directional network actions and keep scope namespaced for safety.
	if p.gatewayNS != "" {
		if ns, ok := payload.Target.Selectors["namespace"]; ok {
			nsStr, ok := ns.(string)
			if ok && strings.ToLower(strings.TrimSpace(nsStr)) == p.gatewayNS {
				if requiresDirectionalTarget(action) && direction != "egress" {
					return nil, policyID, "gateway_policy_direction_invalid", fmt.Errorf("direction: %s", payload.Target.Direction)
				}
				if scope != "" && scope != "namespace" && scope != "tenant" && scope != "region" {
					return nil, policyID, "gateway_policy_scope_invalid", fmt.Errorf("scope: %s", payload.Target.Scope)
				}
			}
		}
	}

	if payload.Guardrails.TTLSeconds == nil || *payload.Guardrails.TTLSeconds <= 0 {
		return nil, policyID, "ttl_seconds_invalid", nil
	}

	ttlDuration := time.Duration(*payload.Guardrails.TTLSeconds) * time.Second
	if ttlDuration <= 0 {
		return nil, policyID, "ttl_seconds_invalid", nil
	}
	if p.maxTTL > 0 && ttlDuration > p.maxTTL {
		return nil, policyID, "ttl_seconds_exceeds_limit", nil
	}

	if len(payload.Target.IPs)+len(payload.Target.CIDRs) == 0 && len(payload.Target.Selectors) == 0 {
		return nil, policyID, "target_missing", nil
	}

	for _, ip := range payload.Target.IPs {
		if net.ParseIP(ip) == nil {
			return nil, policyID, "invalid_ip_target", fmt.Errorf("ip: %s", ip)
		}
	}

	for _, cidr := range payload.Target.CIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, policyID, "invalid_cidr_target", err
		}
		if p.maxCIDRPrefix > 0 && network != nil {
			ones, bits := network.Mask.Size()
			if bits == 32 && ones < p.maxCIDRPrefix {
				return nil, policyID, "cidr_prefix_too_broad", fmt.Errorf("cidr: %s /%d", cidr, ones)
			}
			if bits == 128 && ones < minIPv6Prefix {
				return nil, policyID, "cidr_prefix_too_broad", fmt.Errorf("cidr: %s /%d", cidr, ones)
			}
		}
	}

	if payload.Guardrails.MaxTargets != nil && *payload.Guardrails.MaxTargets > 0 {
		totalTargets := len(payload.Target.IPs) + len(payload.Target.CIDRs)
		if totalTargets > *payload.Guardrails.MaxTargets {
			return nil, policyID, "max_targets_exceeded", nil
		}
	}

	if payload.Guardrails.FastPathTTLSeconds != nil {
		if *payload.Guardrails.FastPathTTLSeconds <= 0 {
			return nil, policyID, "fast_path_ttl_invalid", nil
		}
	}
	if payload.Guardrails.FastPathSignalsRequired != nil {
		if *payload.Guardrails.FastPathSignalsRequired <= 0 {
			return nil, policyID, "fast_path_signals_invalid", nil
		}
	}
	if payload.Guardrails.FastPathConfidenceMin != nil {
		if *payload.Guardrails.FastPathConfidenceMin < 0 || *payload.Guardrails.FastPathConfidenceMin > 1 {
			return nil, policyID, "fast_path_confidence_invalid", nil
		}
	}

	allowlistEntries := payload.Guardrails.Allowlist.Entries()
	if allowlistEntries != nil {
		if len(allowlistEntries) == 0 {
			return nil, policyID, "allowlist_empty", nil
		}
		allowlistSpec, err := parseAllowlist(allowlistEntries)
		if err != nil {
			return nil, policyID, "allowlist_invalid", err
		}
		if err := ensureTargetsOutsideAllowlist(allowlistSpec, payload.Target); err != nil {
			return nil, policyID, "target_overlaps_allowlist", err
		}
	}

	blocksToExpire := int((ttlDuration + p.blockDuration - 1) / p.blockDuration)
	if blocksToExpire <= 0 {
		blocksToExpire = 1
	}
	expirationHeight := int64(height) + int64(blocksToExpire)

	hash := sha256.Sum256(normalizedRaw)

	evt := &pb.PolicyUpdateEvent{
		PolicyId: policyID,
		Action:   controlAction,
		RuleType: ruleType,
		RuleData: normalizedRaw,
		RuleHash: hash[:],
		// RequiresAck is an execution/telemetry concern (did an agent apply it),
		// not the same thing as manual-approval staging.
		RequiresAck:      payload.Guardrails.RequiresAck,
		RollbackPolicyId: rollbackPolicyID,
		Timestamp:        blockTime.Unix(),
		EffectiveHeight:  int64(height),
		ExpirationHeight: expirationHeight,
	}

	return evt, policyID, "", nil
}

func requiresDirectionalTarget(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "drop", "reject":
		return true
	default:
		return false
	}
}

func enrichPolicyPayloadWithContract(raw []byte, contract commandContract) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	metadata := ensureNestedMap(root, "metadata")
	trace := ensureNestedMap(root, "trace")
	params := ensureNestedMap(root, "params")

	setString(root, "policy_id", contract.PolicyID)
	setString(root, "command_id", contract.CommandID)
	setString(root, "request_id", contract.RequestID)
	setString(root, "trace_id", contract.TraceID)
	setString(root, "workflow_id", contract.WorkflowID)
	setString(root, "idempotency_key", contract.IdempotencyKey)
	setString(root, "action", contract.ActionType)
	setString(root, "control_action", contract.ControlAction)
	setString(root, "rule_type", contract.RuleType)
	setString(root, "scope_identifier", contract.ScopeID)
	setString(root, "tenant", contract.Tenant)
	setString(root, "region", contract.Region)
	setString(root, "command_schema_version", contract.SchemaVersion)
	setInt(root, "issued_at_ms", contract.IssuedAtMs)
	setInt(root, "ttl_seconds", contract.TTLSeconds)

	setString(metadata, "policy_id", contract.PolicyID)
	setString(metadata, "command_id", contract.CommandID)
	setString(metadata, "request_id", contract.RequestID)
	setString(metadata, "trace_id", contract.TraceID)
	setString(metadata, "workflow_id", contract.WorkflowID)
	setString(metadata, "idempotency_key", contract.IdempotencyKey)
	setString(metadata, "scope_identifier", contract.ScopeID)
	setString(metadata, "tenant", contract.Tenant)
	setString(metadata, "region", contract.Region)
	setString(metadata, "command_schema_version", contract.SchemaVersion)
	setInt(metadata, "issued_at_ms", contract.IssuedAtMs)
	setInt(metadata, "ttl_seconds", contract.TTLSeconds)

	setString(trace, "id", contract.TraceID)
	setString(trace, "trace_id", contract.TraceID)
	setString(trace, "policy_id", contract.PolicyID)
	setString(trace, "command_id", contract.CommandID)
	setString(trace, "request_id", contract.RequestID)
	setString(trace, "workflow_id", contract.WorkflowID)
	setString(trace, "scope_identifier", contract.ScopeID)
	setString(trace, "tenant", contract.Tenant)
	setString(trace, "region", contract.Region)
	setInt(trace, "issued_at_ms", contract.IssuedAtMs)

	setString(params, "policy_id", contract.PolicyID)
	setString(params, "command_id", contract.CommandID)
	setString(params, "request_id", contract.RequestID)
	setString(params, "trace_id", contract.TraceID)
	setString(params, "workflow_id", contract.WorkflowID)

	normalized, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w", err)
	}
	return normalized, nil
}

func ensureNestedMap(root map[string]interface{}, key string) map[string]interface{} {
	if root == nil || strings.TrimSpace(key) == "" {
		return map[string]interface{}{}
	}
	if existing, ok := root[key].(map[string]interface{}); ok && existing != nil {
		return existing
	}
	created := map[string]interface{}{}
	root[key] = created
	return created
}

func setString(target map[string]interface{}, key, value string) {
	if target == nil || strings.TrimSpace(key) == "" {
		return
	}
	if strings.TrimSpace(value) == "" {
		return
	}
	target[key] = strings.TrimSpace(value)
}

func setInt(target map[string]interface{}, key string, value int64) {
	if target == nil || strings.TrimSpace(key) == "" {
		return
	}
	if value <= 0 {
		return
	}
	target[key] = value
}

func (p *policyPublisher) guardrailReject(ctx context.Context, policyID string, height uint64, reason string, err error, payload []byte) {
	if reason == "" && err != nil {
		reason = err.Error()
	}
	p.recordGuardrail(reason)
	if p.logger != nil {
		fields := []zap.Field{
			utils.ZapString("reason", reason),
			utils.ZapUint64("height", height),
		}
		if policyID != "" {
			fields = append(fields, utils.ZapString("policy_id", policyID))
		}
		if err != nil {
			fields = append(fields, utils.ZapError(err))
		}
		p.logger.WarnContext(ctx, "Policy guardrail rejection", fields...)
	}
	if p.audit != nil {
		entry := map[string]interface{}{
			"reason": reason,
			"height": height,
		}
		if policyID != "" {
			entry["policy_id"] = policyID
		}
		if err != nil {
			entry["error"] = err.Error()
		}
		_ = p.audit.Warn("policy_guardrail_reject", entry)
	}
	if p.trace != nil && policyID != "" {
		traceID := extractTraceIDForGuardrail(payload)
		if traceID == "" {
			traceID = "synthetic:policy:" + policyID
		}
		p.trace.Record(policytrace.Marker{
			Stage:       "t_guardrail_rejected",
			PolicyID:    policyID,
			TraceID:     traceID,
			Reason:      reason,
			TimestampMs: time.Now().UnixMilli(),
		})
	}

	if p.dlqEnabled {
		p.emitDLQ(ctx, policyID, reason, height, err, payload)
	}
}

func (p *policyPublisher) recordGuardrail(reason string) {
	if reason == "" {
		reason = "unknown"
	}
	p.guardrailMu.Lock()
	p.guardrailHits[reason]++
	p.guardrailMu.Unlock()
}

func (p *policyPublisher) recordDedupeSuppressed(reason string) {
	if p == nil {
		return
	}
	reason = strings.TrimSpace(strings.ToLower(reason))
	if reason == "" {
		reason = "unknown"
	}
	p.dedupeMu.Lock()
	if p.dedupeSuppressed == nil {
		p.dedupeSuppressed = make(map[string]uint64)
	}
	p.dedupeSuppressed[reason]++
	p.dedupeMu.Unlock()
}

type policyPublisherStats struct {
	DedupeSuppressedByReason map[string]uint64
}

func (p *policyPublisher) statsSnapshot() policyPublisherStats {
	if p == nil {
		return policyPublisherStats{}
	}
	out := policyPublisherStats{
		DedupeSuppressedByReason: make(map[string]uint64),
	}
	p.dedupeMu.Lock()
	for reason, count := range p.dedupeSuppressed {
		out.DedupeSuppressedByReason[reason] = count
	}
	p.dedupeMu.Unlock()
	return out
}

func (p *policyPublisher) emitDLQ(ctx context.Context, policyID, reason string, height uint64, guardrailErr error, payload []byte) {
	if p.producer == nil || p.dlqTopic == "" {
		return
	}

	record := policyDLQRecord{
		Timestamp:      time.Now().UTC(),
		Reason:         reason,
		PolicyID:       policyID,
		Height:         height,
		GuardrailStage: "policy_publisher",
	}

	if guardrailErr != nil {
		record.Error = guardrailErr.Error()
	}

	if len(payload) > 0 {
		record.PayloadBase64 = base64.StdEncoding.EncodeToString(payload)
	}

	body, err := json.Marshal(record)
	if err != nil {
		p.dlqFailures.Add(1)
		if p.logger != nil {
			p.logger.WarnContext(ctx, "Failed to encode policy DLQ payload",
				utils.ZapError(err),
				utils.ZapString("policy_id", policyID))
		}
		return
	}

	headers := []sarama.RecordHeader{
		{Key: []byte("type"), Value: []byte("policy_guardrail")},
		{Key: []byte("reason"), Value: []byte(reason)},
	}

	if policyID != "" {
		headers = append(headers, sarama.RecordHeader{Key: []byte("policy_id"), Value: []byte(policyID)})
	}

	var key sarama.Encoder
	if policyID != "" {
		key = sarama.StringEncoder(policyID)
	} else {
		key = sarama.StringEncoder(fmt.Sprintf("guardrail:%s", reason))
	}

	if _, _, err := p.producer.PublishDLQ(ctx, p.dlqTopic, key, body, headers); err != nil {
		p.dlqFailures.Add(1)
		if p.logger != nil {
			p.logger.WarnContext(ctx, "Failed to publish policy DLQ payload",
				utils.ZapError(err),
				utils.ZapString("topic", p.dlqTopic))
		}
		if p.audit != nil {
			_ = p.audit.Warn("policy_dlq_publish_failed", map[string]interface{}{
				"policy_id": policyID,
				"reason":    reason,
				"error":     err.Error(),
			})
		}
		return
	}

	p.dlqSuccesses.Add(1)
	if p.audit != nil {
		_ = p.audit.Info("policy_dlq_enqueued", map[string]interface{}{
			"policy_id": policyID,
			"reason":    reason,
			"topic":     p.dlqTopic,
		})
	}
}

type policyParams struct {
	SchemaVersion    int              `json:"schema_version"`
	PolicyID         string           `json:"policy_id"`
	RollbackPolicyID string           `json:"rollback_policy_id"`
	ControlAction    string           `json:"control_action"`
	Tenant           string           `json:"tenant"`
	Region           string           `json:"region"`
	RuleType         string           `json:"rule_type"`
	Action           string           `json:"action"`
	Target           policyTarget     `json:"target"`
	Guardrails       policyGuardrails `json:"guardrails"`
}

func (p policyParams) getTTLSeconds() int {
	if p.Guardrails.TTLSeconds == nil {
		return 0
	}
	return *p.Guardrails.TTLSeconds
}

type policyTarget struct {
	IPs       []string               `json:"ips"`
	CIDRs     []string               `json:"cidrs"`
	Ports     []int                  `json:"ports"`
	Protocols []string               `json:"protocols"`
	Direction string                 `json:"direction"`
	Scope     string                 `json:"scope"`
	TenantID  string                 `json:"tenant_id"`
	Region    string                 `json:"region"`
	Selectors map[string]interface{} `json:"selectors"`
}

type policyGuardrails struct {
	TTLSeconds              *int            `json:"ttl_seconds"`
	CIDRMaxPrefixLen        *int            `json:"cidr_max_prefix_len"`
	ApprovalRequired        bool            `json:"approval_required"`
	RequiresAck             bool            `json:"requires_ack"`
	FastPathEnabled         bool            `json:"fast_path_enabled"`
	FastPathCanary          bool            `json:"fast_path_canary_scope"`
	FastPathTTLSeconds      *int64          `json:"fast_path_ttl_seconds"`
	FastPathSignalsRequired *int64          `json:"fast_path_signals_required"`
	FastPathConfidenceMin   *float64        `json:"fast_path_confidence_min"`
	DryRun                  bool            `json:"dry_run"`
	CanaryScope             bool            `json:"canary_scope"`
	MaxTargets              *int            `json:"max_targets"`
	Allowlist               policyAllowlist `json:"allowlist"`
}

type policyAllowlist struct {
	IPs        []string
	CIDRs      []string
	Namespaces []string
}

func (a *policyAllowlist) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*a = policyAllowlist{}
		return nil
	}

	var flat []string
	if err := json.Unmarshal(data, &flat); err == nil {
		normalized, err := normalizeFlatAllowlist(flat)
		if err != nil {
			return err
		}
		*a = normalized
		return nil
	}

	var obj struct {
		IPs        []string `json:"ips"`
		CIDRs      []string `json:"cidrs"`
		Namespaces []string `json:"namespaces"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("allowlist must be list or object: %w", err)
	}
	*a = policyAllowlist{
		IPs:        sanitizeStringSlice(obj.IPs),
		CIDRs:      sanitizeStringSlice(obj.CIDRs),
		Namespaces: sanitizeStringSlice(obj.Namespaces),
	}
	return nil
}

func (a policyAllowlist) Entries() []string {
	total := len(a.IPs) + len(a.CIDRs) + len(a.Namespaces)
	if total == 0 {
		return nil
	}
	out := make([]string, 0, total)
	out = append(out, a.IPs...)
	out = append(out, a.CIDRs...)
	for _, ns := range a.Namespaces {
		out = append(out, "ns:"+ns)
	}
	return out
}

func normalizeFlatAllowlist(entries []string) (policyAllowlist, error) {
	var out policyAllowlist
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return policyAllowlist{}, fmt.Errorf("empty allowlist entry")
		}
		if strings.HasPrefix(strings.ToLower(entry), "ns:") {
			ns := strings.TrimSpace(entry[3:])
			if ns == "" {
				return policyAllowlist{}, fmt.Errorf("empty allowlist namespace entry")
			}
			out.Namespaces = append(out.Namespaces, ns)
			continue
		}
		if strings.Contains(entry, "/") {
			out.CIDRs = append(out.CIDRs, entry)
			continue
		}
		out.IPs = append(out.IPs, entry)
	}
	return out, nil
}

func sanitizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func extractTraceIDForGuardrail(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(payload, &obj); err != nil {
		return ""
	}

	if traceID := extractTraceIDFromMap(obj); traceID != "" {
		return traceID
	}
	if params, ok := obj["params"].(map[string]interface{}); ok {
		return extractTraceIDFromMap(params)
	}
	return ""
}

func extractTraceIDFromMap(obj map[string]interface{}) string {
	if obj == nil {
		return ""
	}
	if traceID, ok := obj["trace_id"].(string); ok && strings.TrimSpace(traceID) != "" {
		return strings.TrimSpace(traceID)
	}
	if metadata, ok := obj["metadata"].(map[string]interface{}); ok {
		if traceID, ok := metadata["trace_id"].(string); ok && strings.TrimSpace(traceID) != "" {
			return strings.TrimSpace(traceID)
		}
	}
	if traceObj, ok := obj["trace"].(map[string]interface{}); ok {
		if traceID, ok := traceObj["trace_id"].(string); ok && strings.TrimSpace(traceID) != "" {
			return strings.TrimSpace(traceID)
		}
		if traceID, ok := traceObj["id"].(string); ok && strings.TrimSpace(traceID) != "" {
			return strings.TrimSpace(traceID)
		}
	}
	return ""
}

type policyRateLimiter struct {
	limit   int
	mu      sync.Mutex
	windows map[int64]int
}

type parsedAllowlist struct {
	prefixes   []netip.Prefix
	namespaces map[string]struct{}
}

func parseAllowlist(entries []string) (parsedAllowlist, error) {
	prefixes := make([]netip.Prefix, 0, len(entries))
	namespaces := make(map[string]struct{})
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return parsedAllowlist{}, fmt.Errorf("empty allowlist entry")
		}
		if strings.HasPrefix(strings.ToLower(entry), "ns:") {
			ns := strings.ToLower(strings.TrimSpace(entry[3:]))
			if ns == "" {
				return parsedAllowlist{}, fmt.Errorf("empty allowlist namespace entry")
			}
			namespaces[ns] = struct{}{}
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return parsedAllowlist{}, fmt.Errorf("invalid allowlist entry %q: %w", entry, err)
			}
			prefix = prefix.Masked()
			if prefix.Addr().Is6() && prefix.Bits() < minIPv6Prefix {
				return parsedAllowlist{}, fmt.Errorf("invalid allowlist entry %q: ipv6 prefix /%d too broad (min /%d)", entry, prefix.Bits(), minIPv6Prefix)
			}
			prefixes = append(prefixes, prefix)
			continue
		}

		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return parsedAllowlist{}, fmt.Errorf("invalid allowlist entry %q: %w", entry, err)
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return parsedAllowlist{prefixes: prefixes, namespaces: namespaces}, nil
}

func ensureTargetsOutsideAllowlist(allow parsedAllowlist, target policyTarget) error {
	for _, ipStr := range target.IPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ipStr))
		if err != nil {
			return fmt.Errorf("invalid ip target %q: %w", ipStr, err)
		}
		for _, prefix := range allow.prefixes {
			if prefix.Contains(addr) {
				return fmt.Errorf("target ip %s is within allowlist %s", ipStr, prefix.String())
			}
		}
	}

	for _, cidrStr := range target.CIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidrStr))
		if err != nil {
			return fmt.Errorf("invalid cidr target %q: %w", cidrStr, err)
		}
		prefix = prefix.Masked()
		for _, allowPrefix := range allow.prefixes {
			if allowPrefix.Overlaps(prefix) {
				return fmt.Errorf("target cidr %s overlaps allowlist %s", cidrStr, allowPrefix.String())
			}
		}
	}
	if len(allow.namespaces) > 0 {
		rawNS, ok := target.Selectors["namespace"]
		if ok {
			if targetNS, ok := rawNS.(string); ok {
				ns := strings.ToLower(strings.TrimSpace(targetNS))
				if _, exists := allow.namespaces[ns]; exists {
					return fmt.Errorf("target namespace %s is in allowlist", targetNS)
				}
			}
		}
	}

	return nil
}

func newPolicyRateLimiter(limit int) *policyRateLimiter {
	return &policyRateLimiter{
		limit:   limit,
		windows: make(map[int64]int),
	}
}

func (r *policyRateLimiter) Allow(ts time.Time, count int) bool {
	if r == nil || r.limit <= 0 {
		return true
	}
	minute := ts.Truncate(time.Minute).Unix()
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.windows[minute]
	if current+count > r.limit {
		return false
	}
	r.windows[minute] = current + count
	for k := range r.windows {
		if k < minute-2 {
			delete(r.windows, k)
		}
	}
	return true
}
