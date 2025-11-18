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

	"backend/pkg/ingest/kafka"
	"backend/pkg/utils"
	pb "backend/proto"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type policyProducer interface {
	PublishPolicy(ctx context.Context, evt *pb.PolicyUpdateEvent) error
	PublishDLQ(ctx context.Context, topic string, key sarama.Encoder, payload []byte, headers []sarama.RecordHeader) (int32, int64, error)
}

type policyPublisher struct {
	producer      policyProducer
	logger        *utils.Logger
	audit         *utils.AuditLogger
	enabled       bool
	topic         string
	maxTTL        time.Duration
	maxCIDRPrefix int
	maxPerBlock   int
	blockDuration time.Duration
	rateLimiter   *policyRateLimiter
	dlqTopic      string
	dlqEnabled    bool
	guardrailMu   sync.Mutex
	guardrailHits map[string]uint64
	dlqSuccesses  atomic.Uint64
	dlqFailures   atomic.Uint64
}

const minIPv6Prefix = 64

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

func newPolicyPublisher(producer *kafka.Producer, cfgMgr *utils.ConfigManager, topic string, logger *utils.Logger, audit *utils.AuditLogger) (*policyPublisher, error) {
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

	dlqTopic := strings.TrimSpace(cfgMgr.GetString("CONTROL_POLICY_DLQ_TOPIC", ""))
	dlqEnabled := cfgMgr.GetBool("POLICY_DLQ_ENABLED", dlqTopic != "")
	if dlqEnabled && dlqTopic == "" {
		dlqEnabled = false
		if logger != nil {
			logger.Warn("Policy DLQ requested but CONTROL_POLICY_DLQ_TOPIC is empty; disabling DLQ")
		}
	}

	pp := &policyPublisher{
		producer:      producer,
		logger:        logger,
		audit:         audit,
		enabled:       enabled,
		topic:         topic,
		maxTTL:        maxTTL,
		maxCIDRPrefix: maxCIDR,
		maxPerBlock:   maxPerBlock,
		blockDuration: blockDuration,
		rateLimiter:   newPolicyRateLimiter(maxPerMinute),
		dlqTopic:      dlqTopic,
		dlqEnabled:    dlqEnabled,
		guardrailHits: make(map[string]uint64),
	}

	if !enabled && logger != nil {
		logger.Info("Policy publishing disabled via POLICY_ENFORCEMENT_ENABLED=false")
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
		if key := buildPolicyDedupKey(raw); key != "" {
			if _, exists := seen[key]; exists {
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
	}
}

func buildPolicyDedupKey(raw []byte) string {
	var params policyParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return ""
	}

	builder := strings.Builder{}
	builder.WriteString(strings.ToLower(strings.TrimSpace(params.RuleType)))
	builder.WriteString("|")
	builder.WriteString(strings.ToLower(strings.TrimSpace(params.Action)))
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

	if params.Guardrails.Allowlist != nil {
		allow := append([]string(nil), params.Guardrails.Allowlist...)
		sort.Strings(allow)
		builder.WriteString(strings.Join(allow, ","))
	}
	builder.WriteString("|")

	if len(params.Target.Selectors) > 0 {
		if selBytes, err := json.Marshal(params.Target.Selectors); err == nil {
			builder.Write(selBytes)
		}
	}

	return builder.String()
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
	if normalizedAction != "drop" && normalizedAction != "reject" {
		return nil, policyID, "action_invalid", fmt.Errorf("action: %s", payload.Action)
	}
	action = normalizedAction

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
		if scope != "cluster" && scope != "namespace" && scope != "node" {
			return nil, policyID, "scope_invalid", fmt.Errorf("scope: %s", payload.Target.Scope)
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

	if payload.Guardrails.Allowlist != nil {
		if len(payload.Guardrails.Allowlist) == 0 {
			return nil, policyID, "allowlist_empty", nil
		}
		allowPrefixes, err := parseAllowlistPrefixes(payload.Guardrails.Allowlist)
		if err != nil {
			return nil, policyID, "allowlist_invalid", err
		}
		if err := ensureTargetsOutsideAllowlist(allowPrefixes, payload.Target); err != nil {
			return nil, policyID, "target_overlaps_allowlist", err
		}
	}

	blocksToExpire := int((ttlDuration + p.blockDuration - 1) / p.blockDuration)
	if blocksToExpire <= 0 {
		blocksToExpire = 1
	}
	expirationHeight := int64(height) + int64(blocksToExpire)

	hash := sha256.Sum256(raw)

	evt := &pb.PolicyUpdateEvent{
		PolicyId:         policyID,
		Action:           action,
		RuleType:         ruleType,
		RuleData:         raw,
		RuleHash:         hash[:],
		RequiresAck:      payload.Guardrails.ApprovalRequired,
		Timestamp:        blockTime.Unix(),
		EffectiveHeight:  int64(height),
		ExpirationHeight: expirationHeight,
	}

	return evt, policyID, "", nil
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
	SchemaVersion int              `json:"schema_version"`
	PolicyID      string           `json:"policy_id"`
	RuleType      string           `json:"rule_type"`
	Action        string           `json:"action"`
	Target        policyTarget     `json:"target"`
	Guardrails    policyGuardrails `json:"guardrails"`
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
	TTLSeconds       *int     `json:"ttl_seconds"`
	CIDRMaxPrefixLen *int     `json:"cidr_max_prefix_len"`
	ApprovalRequired bool     `json:"approval_required"`
	DryRun           bool     `json:"dry_run"`
	CanaryScope      bool     `json:"canary_scope"`
	MaxTargets       *int     `json:"max_targets"`
	Allowlist        []string `json:"allowlist"`
}

type policyRateLimiter struct {
	limit   int
	mu      sync.Mutex
	windows map[int64]int
}

func parseAllowlistPrefixes(entries []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(entries))
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return nil, fmt.Errorf("empty allowlist entry")
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid allowlist entry %q: %w", entry, err)
			}
			prefix = prefix.Masked()
			if prefix.Addr().Is6() && prefix.Bits() < minIPv6Prefix {
				return nil, fmt.Errorf("invalid allowlist entry %q: ipv6 prefix /%d too broad (min /%d)", entry, prefix.Bits(), minIPv6Prefix)
			}
			prefixes = append(prefixes, prefix)
			continue
		}

		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid allowlist entry %q: %w", entry, err)
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

func ensureTargetsOutsideAllowlist(allow []netip.Prefix, target policyTarget) error {
	for _, ipStr := range target.IPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(ipStr))
		if err != nil {
			return fmt.Errorf("invalid ip target %q: %w", ipStr, err)
		}
		for _, prefix := range allow {
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
		for _, allowPrefix := range allow {
			if allowPrefix.Overlaps(prefix) {
				return fmt.Errorf("target cidr %s overlaps allowlist %s", cidrStr, allowPrefix.String())
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
