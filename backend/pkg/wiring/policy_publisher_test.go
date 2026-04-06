package wiring

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"

	"backend/pkg/control/policytrace"
	pb "backend/proto"
)

type fakePolicyProducer struct {
	published   int
	routingKeys []string
	events      []*pb.PolicyUpdateEvent
}

func (f *fakePolicyProducer) PublishPolicy(ctx context.Context, evt *pb.PolicyUpdateEvent) error {
	f.published++
	f.events = append(f.events, evt)
	return nil
}

func (f *fakePolicyProducer) PublishPolicyWithAck(ctx context.Context, evt *pb.PolicyUpdateEvent) (int32, int64, error) {
	f.published++
	f.events = append(f.events, evt)
	return 0, 0, nil
}

func (f *fakePolicyProducer) PublishPolicyWithRoutingKey(ctx context.Context, evt *pb.PolicyUpdateEvent, routingKey string) (int32, int64, error) {
	f.published++
	f.routingKeys = append(f.routingKeys, routingKey)
	f.events = append(f.events, evt)
	return 0, 0, nil
}

func (f *fakePolicyProducer) PublishDLQ(ctx context.Context, topic string, key sarama.Encoder, payload []byte, headers []sarama.RecordHeader) (int32, int64, error) {
	return 0, 0, nil
}

func TestPolicyPublisher_DedupWindowSkipsDuplicatePolicyID(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	payload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "11111111-1111-4111-8111-111111111111",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.8"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	ctx := context.Background()
	now := time.Now().Unix()
	pp.Publish(ctx, 101, now, 1, [][]byte{payload})
	pp.Publish(ctx, 102, now+1, 1, [][]byte{payload})

	if fp.published != 1 {
		t.Fatalf("expected 1 published policy with dedupe window, got %d", fp.published)
	}
}

func TestPolicyPublisher_DedupWindowExpires(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		dedupeWindow:  1 * time.Second,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	payload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "22222222-2222-4222-8222-222222222222",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.9"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	ctx := context.Background()
	now := time.Now().Unix()
	pp.Publish(ctx, 201, now, 1, [][]byte{payload})
	pp.Publish(ctx, 202, now+2, 1, [][]byte{payload})

	if fp.published != 2 {
		t.Fatalf("expected 2 published policies after dedupe window expiry, got %d", fp.published)
	}
}

func TestPolicyPublisher_DedupWindowDoesNotSuppressDistinctCommandIDs(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	payloadA := []byte(`{
	  "schema_version": 1,
	  "policy_id": "2a222222-2222-4222-8222-222222222222",
	  "command_id": "cmd-a",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.9"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)
	payloadB := []byte(`{
	  "schema_version": 1,
	  "policy_id": "2a222222-2222-4222-8222-222222222222",
	  "command_id": "cmd-b",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.9"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	ctx := context.Background()
	now := time.Now().Unix()
	pp.Publish(ctx, 205, now, 1, [][]byte{payloadA})
	pp.Publish(ctx, 206, now+1, 1, [][]byte{payloadB})

	if fp.published != 2 {
		t.Fatalf("expected 2 published policies for distinct command ids, got %d", fp.published)
	}
}

func TestPolicyPublisher_DedupWindowUsesNestedIdentityFields(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	// command_id only exists under metadata; dedupe must still treat these as distinct.
	payloadA := []byte(`{
	  "schema_version": 1,
	  "policy_id": "2b222222-2222-4222-8222-222222222222",
	  "rule_type": "block",
	  "action": "drop",
	  "metadata": { "command_id": "nested-cmd-a", "request_id": "rq-a" },
	  "target": { "ips": ["10.0.0.10"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)
	payloadB := []byte(`{
	  "schema_version": 1,
	  "policy_id": "2b222222-2222-4222-8222-222222222222",
	  "rule_type": "block",
	  "action": "drop",
	  "metadata": { "command_id": "nested-cmd-b", "request_id": "rq-b" },
	  "target": { "ips": ["10.0.0.10"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	ctx := context.Background()
	now := time.Now().Unix()
	pp.Publish(ctx, 207, now, 1, [][]byte{payloadA})
	pp.Publish(ctx, 208, now+1, 1, [][]byte{payloadB})

	if fp.published != 2 {
		t.Fatalf("expected 2 published policies for distinct nested command ids, got %d", fp.published)
	}
}

func TestPolicyPublisher_DedupeSuppressedStats(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:         fp,
		enabled:          true,
		dedupeWindow:     5 * time.Minute,
		publishedAt:      map[string]time.Time{},
		dedupeSuppressed: map[string]uint64{},
		maxCacheSize:     1024,
		blockDuration:    time.Second,
		rateLimiter:      newPolicyRateLimiter(0),
	}
	payload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "3c333333-3333-4333-8333-333333333333",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.20"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)
	ctx := context.Background()
	now := time.Now().Unix()
	pp.Publish(ctx, 209, now, 1, [][]byte{payload})
	pp.Publish(ctx, 210, now+1, 1, [][]byte{payload})
	if fp.published != 1 {
		t.Fatalf("expected dedupe to suppress second publish, published=%d", fp.published)
	}
	stats := pp.statsSnapshot()
	if stats.DedupeSuppressedByReason["within_window"] != 1 {
		t.Fatalf("expected within_window suppressed count=1, got=%d", stats.DedupeSuppressedByReason["within_window"])
	}
}

func TestPolicyPublisher_PublishFromOutboxUsesScopeRoutingKeyWhenEnabled(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		topic:         "control.policy.v2",
		scopeRouting:  true,
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	payload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "33333333-3333-4333-8333-333333333333",
	  "rule_type": "block",
	  "action": "drop",
	  "target": {
	    "ips": ["10.0.0.10"],
	    "direction": "ingress",
	    "scope": "namespace",
	    "selectors": {"namespace": "prod-gateway"}
	  },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	policyID, partition, offset, err := pp.PublishFromOutbox(context.Background(), 301, time.Now().Unix(), payload)
	if err != nil {
		t.Fatalf("PublishFromOutbox failed: %v", err)
	}
	if policyID != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("unexpected policy id %q", policyID)
	}
	if partition != 0 || offset != 0 {
		t.Fatalf("unexpected Kafka coordinates partition=%d offset=%d", partition, offset)
	}
	if len(fp.routingKeys) != 1 {
		t.Fatalf("expected 1 routing key, got %d", len(fp.routingKeys))
	}
	if fp.routingKeys[0] != "namespace:prod-gateway" {
		t.Fatalf("expected namespace routing key, got %q", fp.routingKeys[0])
	}
}

func TestPolicyPublisher_PublishFromOutboxKeepsLegacyRoutingWhenScopeRoutingDisabled(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		topic:         "control.policy.v2",
		scopeRouting:  false,
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	payload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "44444444-4444-4444-8444-444444444444",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.11"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 401, time.Now().Unix(), payload); err != nil {
		t.Fatalf("PublishFromOutbox failed: %v", err)
	}
	if len(fp.routingKeys) != 1 {
		t.Fatalf("expected 1 PublishPolicyWithRoutingKey call, got %d", len(fp.routingKeys))
	}
	if fp.routingKeys[0] != "" {
		t.Fatalf("expected legacy empty routing key for v1, got %q", fp.routingKeys[0])
	}
}

func TestPolicyPublisher_PublishFromOutboxMapsBlockActionsToControlIntent(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		topic:         "control.policy.v2",
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	addPayload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "66666666-6666-4666-8666-666666666661",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.12"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)
	removePayload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "66666666-6666-4666-8666-666666666662",
	  "rollback_policy_id": "66666666-6666-4666-8666-666666666661",
	  "rule_type": "block",
	  "action": "remove",
	  "guardrails": { "requires_ack": true }
	}`)

	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 501, time.Now().Unix(), addPayload); err != nil {
		t.Fatalf("PublishFromOutbox(add) failed: %v", err)
	}
	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 502, time.Now().Unix(), removePayload); err != nil {
		t.Fatalf("PublishFromOutbox(remove) failed: %v", err)
	}
	if len(fp.events) != 2 {
		t.Fatalf("expected 2 published events, got %d", len(fp.events))
	}
	if fp.events[0].GetAction() != "add" {
		t.Fatalf("expected control action add, got %q", fp.events[0].GetAction())
	}
	if fp.events[1].GetAction() != "remove" {
		t.Fatalf("expected control action remove, got %q", fp.events[1].GetAction())
	}
	if fp.events[1].GetRollbackPolicyId() != "66666666-6666-4666-8666-666666666661" {
		t.Fatalf("expected rollback policy id to propagate, got %q", fp.events[1].GetRollbackPolicyId())
	}
	if fp.events[1].GetExpirationHeight() != 0 {
		t.Fatalf("expected remove expiration height 0, got %d", fp.events[1].GetExpirationHeight())
	}
}

func TestPolicyPublisher_PublishFromOutboxSupportsApprovalDecisionActions(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		topic:         "control.policy.v2",
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	approvePayload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "66666666-6666-4666-8666-666666666663",
	  "control_action": "approve",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.14"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true, "approval_required": true }
	}`)
	rejectPayload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "66666666-6666-4666-8666-666666666664",
	  "control_action": "reject",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.15"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true, "approval_required": true }
	}`)

	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 503, time.Now().Unix(), approvePayload); err != nil {
		t.Fatalf("PublishFromOutbox(approve) failed: %v", err)
	}
	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 504, time.Now().Unix(), rejectPayload); err != nil {
		t.Fatalf("PublishFromOutbox(reject) failed: %v", err)
	}
	if len(fp.events) != 2 {
		t.Fatalf("expected 2 published events, got %d", len(fp.events))
	}
	if fp.events[0].GetAction() != "approve" {
		t.Fatalf("expected control action approve, got %q", fp.events[0].GetAction())
	}
	if fp.events[1].GetAction() != "reject" {
		t.Fatalf("expected control action reject, got %q", fp.events[1].GetAction())
	}
}

func TestPolicyPublisher_PublishFromOutboxAcceptsTenantAndRegionScopes(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		topic:         "control.policy.v2",
		scopeRouting:  true,
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	tenantPayload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "77777777-7777-4777-8777-777777777771",
	  "rule_type": "block",
	  "action": "drop",
	  "tenant": "tenant-a",
	  "target": { "ips": ["10.0.0.13"], "direction": "ingress", "scope": "tenant" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)
	regionPayload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "77777777-7777-4777-8777-777777777772",
	  "rule_type": "block",
	  "action": "drop",
	  "region": "us-west",
	  "target": { "cidrs": ["10.0.1.0/24"], "direction": "ingress", "scope": "region" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 601, time.Now().Unix(), tenantPayload); err != nil {
		t.Fatalf("PublishFromOutbox(tenant) failed: %v", err)
	}
	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 602, time.Now().Unix(), regionPayload); err != nil {
		t.Fatalf("PublishFromOutbox(region) failed: %v", err)
	}
	if len(fp.routingKeys) != 2 {
		t.Fatalf("expected 2 routing keys, got %d", len(fp.routingKeys))
	}
	if fp.routingKeys[0] != "tenant:tenant-a" {
		t.Fatalf("expected tenant routing key, got %q", fp.routingKeys[0])
	}
	if fp.routingKeys[1] != "region:us-west" {
		t.Fatalf("expected region routing key, got %q", fp.routingKeys[1])
	}
}

func TestPolicyPublisher_PublishFromOutboxAcceptsGatewayTenantAndRegionScopes(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		topic:         "control.policy.v2",
		scopeRouting:  true,
		gatewayNS:     "cybermesh-gateway",
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	tenantPayload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "77777777-7777-4777-8777-777777777773",
	  "rule_type": "block",
	  "action": "drop",
	  "tenant": "tenant-a",
	  "target": {
	    "cidrs": ["10.0.2.0/24"],
	    "direction": "egress",
	    "scope": "tenant",
	    "selectors": {"namespace": "cybermesh-gateway"}
	  },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)
	regionPayload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "77777777-7777-4777-8777-777777777774",
	  "rule_type": "block",
	  "action": "drop",
	  "region": "us-west",
	  "target": {
	    "cidrs": ["10.0.3.0/24"],
	    "direction": "egress",
	    "scope": "region",
	    "selectors": {"namespace": "cybermesh-gateway"}
	  },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 603, time.Now().Unix(), tenantPayload); err != nil {
		t.Fatalf("PublishFromOutbox(gateway tenant) failed: %v", err)
	}
	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 604, time.Now().Unix(), regionPayload); err != nil {
		t.Fatalf("PublishFromOutbox(gateway region) failed: %v", err)
	}
	if len(fp.routingKeys) != 2 {
		t.Fatalf("expected 2 routing keys, got %d", len(fp.routingKeys))
	}
	if fp.routingKeys[0] != "tenant:tenant-a" {
		t.Fatalf("expected tenant routing key, got %q", fp.routingKeys[0])
	}
	if fp.routingKeys[1] != "region:us-west" {
		t.Fatalf("expected region routing key, got %q", fp.routingKeys[1])
	}
}

func TestPolicyPublisher_GuardrailRejectRecordsTraceMarker(t *testing.T) {
	trace := policytrace.NewCollector(16, 8)
	pp := &policyPublisher{
		trace:         trace,
		guardrailHits: map[string]uint64{},
	}

	payload := []byte(`{
	  "policy_id": "88888888-8888-4888-8888-888888888888",
	  "metadata": { "trace_id": "trace-abc-123" }
	}`)
	pp.guardrailReject(context.Background(), "88888888-8888-4888-8888-888888888888", 701, "target_overlaps_allowlist", nil, payload)

	markers := trace.GetPolicy("88888888-8888-4888-8888-888888888888")
	if len(markers) != 1 {
		t.Fatalf("expected 1 trace marker, got %d", len(markers))
	}
	if markers[0].Stage != "t_guardrail_rejected" {
		t.Fatalf("expected t_guardrail_rejected marker, got %q", markers[0].Stage)
	}
	if markers[0].TraceID != "trace-abc-123" {
		t.Fatalf("expected trace ID from payload, got %q", markers[0].TraceID)
	}
}

func TestPolicyPublisher_DedupWindowDoesNotSuppressDifferentPolicyIDs(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	payloadA := []byte(`{
	  "schema_version": 1,
	  "policy_id": "90111111-1111-4111-8111-111111111111",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.8"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)
	payloadB := []byte(`{
	  "schema_version": 1,
	  "policy_id": "90222222-2222-4222-8222-222222222222",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.0.0.8"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	ctx := context.Background()
	now := time.Now().Unix()
	pp.Publish(ctx, 1001, now, 1, [][]byte{payloadA})
	pp.Publish(ctx, 1002, now+1, 1, [][]byte{payloadB})

	if fp.published != 2 {
		t.Fatalf("expected 2 published policies for different policy IDs, got %d", fp.published)
	}
}

func TestPolicyPublisher_ParseAllowlistSupportsStructuredAndLegacyFlat(t *testing.T) {
	structured, err := parseAllowlist([]string{"192.0.2.5", "198.51.100.0/24", "ns:prod"})
	if err != nil {
		t.Fatalf("parseAllowlist(structured-like entries) error: %v", err)
	}
	if len(structured.prefixes) != 2 {
		t.Fatalf("expected 2 prefix entries, got %d", len(structured.prefixes))
	}
	if _, ok := structured.namespaces["prod"]; !ok {
		t.Fatalf("expected namespace allowlist to include prod")
	}

	var guard policyGuardrails
	if err := json.Unmarshal([]byte(`{"allowlist":["203.0.113.7","203.0.113.0/24","ns:ops"]}`), &guard); err != nil {
		t.Fatalf("legacy allowlist unmarshal failed: %v", err)
	}
	entries := guard.Allowlist.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 normalized allowlist entries, got %d", len(entries))
	}
}

func TestPolicyPublisher_PrepareEventEnrichesContractIDsWhenMissing(t *testing.T) {
	pp := &policyPublisher{
		enabled:       true,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	raw := []byte(`{
	  "schema_version": 1,
	  "policy_id": "91111111-1111-4111-8111-111111111111",
	  "rule_type": "block",
	  "action": "drop",
	  "target": { "ips": ["10.1.0.8"], "direction": "ingress", "scope": "cluster" },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	evt, _, reason, err := pp.prepareEvent(100, time.Unix(1700000000, 0).UTC(), raw)
	if err != nil || reason != "" {
		t.Fatalf("prepareEvent failed: reason=%s err=%v", reason, err)
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(evt.GetRuleData(), &payload); err != nil {
		t.Fatalf("rule_data must remain valid JSON: %v", err)
	}

	get := func(key string) string {
		v, _ := payload[key].(string)
		return v
	}
	if get("command_id") == "" {
		t.Fatal("expected command_id in top-level rule_data")
	}
	if get("request_id") == "" {
		t.Fatal("expected request_id in top-level rule_data")
	}
	if get("trace_id") == "" {
		t.Fatal("expected trace_id in top-level rule_data")
	}
	if get("idempotency_key") == "" {
		t.Fatal("expected idempotency_key in top-level rule_data")
	}

	traceMap, _ := payload["trace"].(map[string]interface{})
	if traceMap == nil {
		t.Fatal("expected trace object in enriched rule_data")
	}
	if traceID, _ := traceMap["trace_id"].(string); traceID == "" {
		t.Fatal("expected trace.trace_id in enriched rule_data")
	}
	if traceID, _ := traceMap["id"].(string); traceID == "" {
		t.Fatal("expected trace.id in enriched rule_data")
	}

	metaMap, _ := payload["metadata"].(map[string]interface{})
	if metaMap == nil {
		t.Fatal("expected metadata object in enriched rule_data")
	}
	if commandID, _ := metaMap["command_id"].(string); commandID == "" {
		t.Fatal("expected metadata.command_id in enriched rule_data")
	}
}

func TestPolicyPublisher_PublishFromOutboxAcceptsAppActions(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		payload    string
		wantAction string
	}{
		{
			name:       "force_reauth",
			action:     "force_reauth",
			wantAction: "add",
			payload:    `{"schema_version":1,"policy_id":"93333333-3333-4333-8333-333333333333","rule_type":"block","action":"force_reauth","target":{"scope":"tenant","selectors":{"tenant_id":"tenant-a","user_id":"user-a"}},"guardrails":{"ttl_seconds":60,"requires_ack":true}}`,
		},
		{
			name:       "disable_export",
			action:     "disable_export",
			wantAction: "add",
			payload:    `{"schema_version":1,"policy_id":"94444444-4444-4444-8444-444444444444","rule_type":"block","action":"disable_export","target":{"scope":"tenant","selectors":{"tenant_id":"tenant-a"}},"guardrails":{"ttl_seconds":60,"requires_ack":true}}`,
		},
		{
			name:       "freeze_user",
			action:     "freeze_user",
			wantAction: "add",
			payload:    `{"schema_version":1,"policy_id":"95555555-5555-4555-8555-555555555555","rule_type":"block","action":"freeze_user","target":{"scope":"tenant","selectors":{"tenant_id":"tenant-a","user_id":"u1"}},"guardrails":{"ttl_seconds":60,"requires_ack":true}}`,
		},
		{
			name:       "freeze_tenant",
			action:     "freeze_tenant",
			wantAction: "add",
			payload:    `{"schema_version":1,"policy_id":"96666666-6666-4666-8666-666666666666","rule_type":"block","action":"freeze_tenant","target":{"scope":"tenant","selectors":{"tenant_id":"tenant-a"}},"guardrails":{"ttl_seconds":60,"requires_ack":true}}`,
		},
		{
			name:       "throttle_action",
			action:     "throttle_action",
			wantAction: "add",
			payload:    `{"schema_version":1,"policy_id":"97777777-7777-4777-8777-777777777777","rule_type":"block","action":"throttle_action","target":{"scope":"tenant","selectors":{"tenant_id":"tenant-a"}},"guardrails":{"ttl_seconds":60,"requires_ack":true}}`,
		},
		{
			name:       "rate_limit",
			action:     "rate_limit",
			wantAction: "add",
			payload:    `{"schema_version":1,"policy_id":"98888888-8888-4888-8888-888888888888","rule_type":"block","action":"rate_limit","target":{"scope":"tenant","selectors":{"tenant_id":"tenant-a"}},"guardrails":{"ttl_seconds":60,"requires_ack":true}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fp := &fakePolicyProducer{}
			pp := &policyPublisher{
				producer:      fp,
				enabled:       true,
				topic:         "control.policy.v2",
				dedupeWindow:  5 * time.Minute,
				publishedAt:   map[string]time.Time{},
				maxCacheSize:  1024,
				blockDuration: time.Second,
				rateLimiter:   newPolicyRateLimiter(0),
			}
			if _, _, _, err := pp.PublishFromOutbox(context.Background(), 700, time.Now().Unix(), []byte(tc.payload)); err != nil {
				t.Fatalf("PublishFromOutbox(%s) failed: %v", tc.action, err)
			}
			if len(fp.events) != 1 {
				t.Fatalf("expected one policy event, got %d", len(fp.events))
			}
			if got := fp.events[0].GetAction(); got != tc.wantAction {
				t.Fatalf("expected control action %s for %s, got %q", tc.wantAction, tc.action, got)
			}
		})
	}
}

func TestPolicyPublisher_GatewayNamespaceRejectsIngressForDirectionalAction(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		topic:         "control.policy.v2",
		gatewayNS:     "cybermesh-gateway",
		guardrailHits: map[string]uint64{},
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	payload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "9a111111-1111-4111-8111-111111111111",
	  "rule_type": "block",
	  "action": "drop",
	  "target": {
	    "ips": ["10.0.0.11"],
	    "direction": "ingress",
	    "scope": "namespace",
	    "selectors": {"namespace": "cybermesh-gateway"}
	  },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	_, _, _, err := pp.PublishFromOutbox(context.Background(), 801, time.Now().Unix(), payload)
	if err == nil {
		t.Fatalf("expected gateway directional guardrail error")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "direction") {
		t.Fatalf("expected direction-related error, got %q", got)
	}
}

func TestPolicyPublisher_GatewayNamespaceAllowsAppActionWithoutDirection(t *testing.T) {
	fp := &fakePolicyProducer{}
	pp := &policyPublisher{
		producer:      fp,
		enabled:       true,
		topic:         "control.policy.v2",
		gatewayNS:     "cybermesh-gateway",
		guardrailHits: map[string]uint64{},
		dedupeWindow:  5 * time.Minute,
		publishedAt:   map[string]time.Time{},
		maxCacheSize:  1024,
		blockDuration: time.Second,
		rateLimiter:   newPolicyRateLimiter(0),
	}

	payload := []byte(`{
	  "schema_version": 1,
	  "policy_id": "9a222222-2222-4222-8222-222222222222",
	  "rule_type": "block",
	  "action": "force_reauth",
	  "target": {
	    "scope": "namespace",
	    "selectors": {"namespace": "cybermesh-gateway", "tenant_id": "tenant-a", "user_id": "u1"}
	  },
	  "guardrails": { "ttl_seconds": 60, "requires_ack": true }
	}`)

	if _, _, _, err := pp.PublishFromOutbox(context.Background(), 802, time.Now().Unix(), payload); err != nil {
		t.Fatalf("expected app action to pass gateway namespace guardrail, got %v", err)
	}
}
