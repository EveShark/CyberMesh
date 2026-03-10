package wiring

import (
	"context"
	"testing"
	"time"

	"github.com/IBM/sarama"

	pb "backend/proto"
)

type fakePolicyProducer struct {
	published   int
	routingKeys []string
}

func (f *fakePolicyProducer) PublishPolicy(ctx context.Context, evt *pb.PolicyUpdateEvent) error {
	f.published++
	return nil
}

func (f *fakePolicyProducer) PublishPolicyWithAck(ctx context.Context, evt *pb.PolicyUpdateEvent) (int32, int64, error) {
	f.published++
	return 0, 0, nil
}

func (f *fakePolicyProducer) PublishPolicyWithRoutingKey(ctx context.Context, evt *pb.PolicyUpdateEvent, routingKey string) (int32, int64, error) {
	f.published++
	f.routingKeys = append(f.routingKeys, routingKey)
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
