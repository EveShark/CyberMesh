package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/CyberMesh/enforcement-agent/internal/ack"
	"github.com/CyberMesh/enforcement-agent/internal/control"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/gateway"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
	"github.com/CyberMesh/enforcement-agent/internal/ratelimit"
	"github.com/CyberMesh/enforcement-agent/internal/state"

	pb "backend/proto"
)

type fakeGatewayDelegate struct {
	applied int
}

func (f *fakeGatewayDelegate) Apply(context.Context, policy.PolicySpec) error {
	f.applied++
	return nil
}
func (f *fakeGatewayDelegate) Remove(context.Context, string) error { return nil }
func (f *fakeGatewayDelegate) List(context.Context) ([]policy.PolicySpec, error) {
	return nil, nil
}

type capturePublisher struct{ payloads []ack.Payload }

func (c *capturePublisher) Publish(_ context.Context, payload ack.Payload) error {
	c.payloads = append(c.payloads, payload)
	return nil
}
func (c *capturePublisher) PublishBatch(_ context.Context, payloads []ack.Payload) error {
	c.payloads = append(c.payloads, payloads...)
	return nil
}
func (c *capturePublisher) Close(context.Context) error { return nil }

func signPolicyEvent(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey, evt *pb.PolicyUpdateEvent) {
	t.Helper()
	if evt == nil {
		t.Fatalf("evt nil")
	}
	sum := sha256.Sum256(evt.RuleData)
	evt.RuleHash = sum[:]
	evt.Pubkey = append([]byte(nil), pub...)
	evt.ProducerId = append([]byte(nil), pub...)
	evt.Alg = "Ed25519"

	signed := &pb.PolicyUpdateEvent{
		PolicyId:         evt.PolicyId,
		Action:           evt.Action,
		RuleType:         evt.RuleType,
		RuleData:         evt.RuleData,
		RuleHash:         evt.RuleHash,
		RequiresAck:      evt.RequiresAck,
		RollbackPolicyId: evt.RollbackPolicyId,
		Timestamp:        evt.Timestamp,
		EffectiveHeight:  evt.EffectiveHeight,
		ExpirationHeight: evt.ExpirationHeight,
		ProducerId:       evt.ProducerId,
	}
	b, err := proto.Marshal(signed)
	if err != nil {
		t.Fatalf("marshal signed: %v", err)
	}
	msg := append([]byte("control.policy.v2"), b...)
	evt.Signature = ed25519.Sign(priv, msg)
}

func TestGatewayBackend_ControllerHandleMessage_AppliesAndAcks(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	trust := policy.NewTrustedKeys(pub)

	tmp := t.TempDir()
	store := state.NewStore(state.Options{PersistPath: filepath.Join(tmp, "state.db")})
	if err := store.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}

	reg := prometheus.NewRegistry()
	rec := metrics.NewRecorder(reg)
	rec.SetBackend("gateway")
	coord, err := ratelimit.Factory("local", ratelimit.Options{Local: &ratelimit.LocalOptions{Counter: store}})
	if err != nil {
		t.Fatalf("ratelimit: %v", err)
	}
	kill := control.NewKillSwitch(false)
	acks := &capturePublisher{}

	delegate := &fakeGatewayDelegate{}
	ge, err := gateway.New(gateway.Config{GatewayNamespace: "cybermesh-gateway", DryRun: true, Logger: zap.NewNop(), Delegate: delegate})
	if err != nil {
		t.Fatalf("gateway new: %v", err)
	}

	ctrl := New(trust, store, ge, rec, coord, kill, zap.NewNop(), Options{
		AckPublisher:         acks,
		ControllerInstanceID: "gw-agent-1",
	})

	rule := map[string]any{
		"policy_id": "11111111-1111-1111-1111-111111111111",
		"rule_type": "block",
		"action":    "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds": 30,
			"dry_run":     true,
		},
		"audit": map[string]any{
			"reason_code": "GW-E2E",
		},
	}
	ruleBytes, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}

	evt := &pb.PolicyUpdateEvent{
		PolicyId:         "11111111-1111-1111-1111-111111111111",
		Action:           "add",
		RuleType:         "block",
		RuleData:         ruleBytes,
		RequiresAck:      true,
		Timestamp:        time.Now().Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 10,
	}
	signPolicyEvent(t, priv, pub, evt)

	raw, err := proto.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal evt: %v", err)
	}
	msg := &sarama.ConsumerMessage{Value: raw}

	if err := ctrl.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if delegate.applied != 1 {
		t.Fatalf("expected Apply called once, got %d", delegate.applied)
	}
	if len(acks.payloads) != 1 {
		t.Fatalf("expected 1 ack payload, got %d", len(acks.payloads))
	}
	if acks.payloads[0].Result != ack.ResultApplied {
		t.Fatalf("expected applied ack, got %s", acks.payloads[0].Result)
	}
}

func TestGatewayBackend_ControllerHandleMessage_DuplicateIsIdempotent(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	trust := policy.NewTrustedKeys(pub)

	tmp := t.TempDir()
	store := state.NewStore(state.Options{PersistPath: filepath.Join(tmp, "state.db")})
	if err := store.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}

	reg := prometheus.NewRegistry()
	rec := metrics.NewRecorder(reg)
	rec.SetBackend("gateway")
	coord, err := ratelimit.Factory("local", ratelimit.Options{Local: &ratelimit.LocalOptions{Counter: store}})
	if err != nil {
		t.Fatalf("ratelimit: %v", err)
	}
	kill := control.NewKillSwitch(false)
	acks := &capturePublisher{}
	delegate := &fakeGatewayDelegate{}
	ge, err := gateway.New(gateway.Config{GatewayNamespace: "cybermesh-gateway", DryRun: true, Logger: zap.NewNop(), Delegate: delegate})
	if err != nil {
		t.Fatalf("gateway new: %v", err)
	}

	ctrl := New(trust, store, ge, rec, coord, kill, zap.NewNop(), Options{
		AckPublisher:         acks,
		ControllerInstanceID: "gw-agent-1",
	})

	rule := map[string]any{
		"policy_id": "44444444-4444-4444-4444-444444444444",
		"rule_type": "block",
		"action":    "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds": 30,
			"dry_run":     true,
		},
	}
	ruleBytes, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}

	evt := &pb.PolicyUpdateEvent{
		PolicyId:         "44444444-4444-4444-4444-444444444444",
		Action:           "add",
		RuleType:         "block",
		RuleData:         ruleBytes,
		RequiresAck:      true,
		Timestamp:        time.Now().Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 10,
	}
	signPolicyEvent(t, priv, pub, evt)
	raw, err := proto.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal evt: %v", err)
	}
	msg := &sarama.ConsumerMessage{Value: raw}

	if err := ctrl.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if err := ctrl.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("second handle: %v", err)
	}

	if delegate.applied != 1 {
		t.Fatalf("expected apply once for duplicate event, got %d", delegate.applied)
	}
	if len(acks.payloads) != 2 {
		t.Fatalf("expected 2 ack payloads, got %d", len(acks.payloads))
	}
	if acks.payloads[1].Result != ack.ResultApplied {
		t.Fatalf("expected duplicate ack to be applied/noop, got %s", acks.payloads[1].Result)
	}
	if acks.payloads[1].ErrorCode != "duplicate_noop" {
		t.Fatalf("expected duplicate_noop error code, got %s", acks.payloads[1].ErrorCode)
	}
}

func TestGatewayBackend_ControllerHandleMessage_RejectsReplayStaleTimestamp(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	trust := policy.NewTrustedKeys(pub)
	tmp := t.TempDir()
	store := state.NewStore(state.Options{PersistPath: filepath.Join(tmp, "state.db")})
	if err := store.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}

	reg := prometheus.NewRegistry()
	rec := metrics.NewRecorder(reg)
	rec.SetBackend("gateway")
	coord, err := ratelimit.Factory("local", ratelimit.Options{Local: &ratelimit.LocalOptions{Counter: store}})
	if err != nil {
		t.Fatalf("ratelimit: %v", err)
	}
	kill := control.NewKillSwitch(false)
	acks := &capturePublisher{}
	delegate := &fakeGatewayDelegate{}
	ge, err := gateway.New(gateway.Config{GatewayNamespace: "cybermesh-gateway", DryRun: true, Logger: zap.NewNop(), Delegate: delegate})
	if err != nil {
		t.Fatalf("gateway new: %v", err)
	}

	ctrl := New(trust, store, ge, rec, coord, kill, zap.NewNop(), Options{
		AckPublisher:         acks,
		ControllerInstanceID: "gw-agent-1",
		ReplayWindow:         1 * time.Minute,
	})

	rule := map[string]any{
		"policy_id": "22222222-2222-2222-2222-222222222222",
		"rule_type": "block",
		"action":    "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds": 30,
			"dry_run":     true,
		},
	}
	ruleBytes, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}

	evt := &pb.PolicyUpdateEvent{
		PolicyId:         "22222222-2222-2222-2222-222222222222",
		Action:           "add",
		RuleType:         "block",
		RuleData:         ruleBytes,
		RequiresAck:      true,
		Timestamp:        time.Now().Add(-10 * time.Minute).Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 10,
	}
	signPolicyEvent(t, priv, pub, evt)
	raw, err := proto.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal evt: %v", err)
	}
	msg := &sarama.ConsumerMessage{Value: raw}
	if err := ctrl.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if delegate.applied != 0 {
		t.Fatalf("expected no apply on stale replay, got %d", delegate.applied)
	}
	if len(acks.payloads) != 1 {
		t.Fatalf("expected ack payload for stale replay")
	}
	if acks.payloads[0].ErrorCode != "replay_stale_timestamp" {
		t.Fatalf("expected replay_stale_timestamp, got %s", acks.payloads[0].ErrorCode)
	}
	if got := testutil.ToFloat64(rec.GatewayReplayCounter().WithLabelValues("replay_stale_timestamp")); got != 1 {
		t.Fatalf("expected gateway replay metric 1, got %v", got)
	}
}

func TestGatewayBackend_ControllerHandleMessage_RejectsReplayFutureTimestamp(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	trust := policy.NewTrustedKeys(pub)
	tmp := t.TempDir()
	store := state.NewStore(state.Options{PersistPath: filepath.Join(tmp, "state.db")})
	if err := store.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}

	reg := prometheus.NewRegistry()
	rec := metrics.NewRecorder(reg)
	rec.SetBackend("gateway")
	coord, err := ratelimit.Factory("local", ratelimit.Options{Local: &ratelimit.LocalOptions{Counter: store}})
	if err != nil {
		t.Fatalf("ratelimit: %v", err)
	}
	kill := control.NewKillSwitch(false)
	acks := &capturePublisher{}
	delegate := &fakeGatewayDelegate{}
	ge, err := gateway.New(gateway.Config{GatewayNamespace: "cybermesh-gateway", DryRun: true, Logger: zap.NewNop(), Delegate: delegate})
	if err != nil {
		t.Fatalf("gateway new: %v", err)
	}

	ctrl := New(trust, store, ge, rec, coord, kill, zap.NewNop(), Options{
		AckPublisher:         acks,
		ControllerInstanceID: "gw-agent-1",
		ReplayWindow:         5 * time.Minute,
		ReplayFutureSkew:     2 * time.Second,
	})

	rule := map[string]any{
		"policy_id": "33333333-3333-3333-3333-333333333333",
		"rule_type": "block",
		"action":    "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds": 30,
			"dry_run":     true,
		},
	}
	ruleBytes, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}

	evt := &pb.PolicyUpdateEvent{
		PolicyId:         "33333333-3333-3333-3333-333333333333",
		Action:           "add",
		RuleType:         "block",
		RuleData:         ruleBytes,
		RequiresAck:      true,
		Timestamp:        time.Now().Add(30 * time.Second).Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 10,
	}
	signPolicyEvent(t, priv, pub, evt)
	raw, err := proto.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal evt: %v", err)
	}
	msg := &sarama.ConsumerMessage{Value: raw}
	if err := ctrl.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if delegate.applied != 0 {
		t.Fatalf("expected no apply on future replay, got %d", delegate.applied)
	}
	if len(acks.payloads) != 1 {
		t.Fatalf("expected ack payload for future replay")
	}
	if acks.payloads[0].ErrorCode != "replay_future_timestamp" {
		t.Fatalf("expected replay_future_timestamp, got %s", acks.payloads[0].ErrorCode)
	}
	if got := testutil.ToFloat64(rec.GatewayReplayCounter().WithLabelValues("replay_future_timestamp")); got != 1 {
		t.Fatalf("expected gateway replay metric 1, got %v", got)
	}
}
