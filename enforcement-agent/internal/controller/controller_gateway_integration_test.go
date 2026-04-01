package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	applied    int
	removed    []string
	applyErrs  []error
	removeErrs []error
}

func (f *fakeGatewayDelegate) Apply(context.Context, policy.PolicySpec) error {
	f.applied++
	if len(f.applyErrs) > 0 {
		err := f.applyErrs[0]
		f.applyErrs = f.applyErrs[1:]
		return err
	}
	return nil
}
func (f *fakeGatewayDelegate) Remove(_ context.Context, policyID string) error {
	f.removed = append(f.removed, policyID)
	if len(f.removeErrs) > 0 {
		err := f.removeErrs[0]
		f.removeErrs = f.removeErrs[1:]
		return err
	}
	return nil
}
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

	duplicateEvt := proto.Clone(evt).(*pb.PolicyUpdateEvent)
	duplicateEvt.Timestamp = time.Now().Add(2 * time.Second).Unix()
	signPolicyEvent(t, priv, pub, duplicateEvt)
	duplicateRaw, err := proto.Marshal(duplicateEvt)
	if err != nil {
		t.Fatalf("marshal duplicate evt: %v", err)
	}
	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: duplicateRaw}); err != nil {
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

func TestGatewayBackend_ControllerHandleMessage_DoesNotPoisonReplayOnTransientApplyFailure(t *testing.T) {
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
	delegate := &fakeGatewayDelegate{
		applyErrs: []error{context.Canceled},
	}
	ge, err := gateway.New(gateway.Config{GatewayNamespace: "cybermesh-gateway", DryRun: true, Logger: zap.NewNop(), Delegate: delegate})
	if err != nil {
		t.Fatalf("gateway new: %v", err)
	}

	ctrl := New(trust, store, ge, rec, coord, kill, zap.NewNop(), Options{
		AckPublisher:         acks,
		ControllerInstanceID: "gw-agent-1",
		ReplayWindow:         5 * time.Minute,
	})

	rule := map[string]any{
		"policy_id": "88888888-8888-4888-8888-888888888888",
		"rule_type": "block",
		"action":    "drop",
		"target": map[string]any{
			"cidrs":     []any{"203.0.113.0/24"},
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
		PolicyId:         "88888888-8888-4888-8888-888888888888",
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

	if err := ctrl.HandleMessage(context.Background(), msg); err == nil {
		t.Fatalf("expected transient apply error on first handle")
	}
	if delegate.applied != 1 {
		t.Fatalf("expected first apply attempt count 1, got %d", delegate.applied)
	}
	if len(acks.payloads) != 1 || acks.payloads[0].ErrorCode != "apply_error" {
		t.Fatalf("expected first ack apply_error, got %#v", acks.payloads)
	}

	// Same Kafka envelope redelivery should be re-attempted (not replay_duplicate).
	if err := ctrl.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("second handle should succeed after transient failure, got %v", err)
	}
	if delegate.applied != 2 {
		t.Fatalf("expected second apply attempt, got %d", delegate.applied)
	}
	if len(acks.payloads) != 2 {
		t.Fatalf("expected 2 ack payloads, got %d", len(acks.payloads))
	}
	if acks.payloads[1].Result != ack.ResultApplied {
		t.Fatalf("expected second ack applied, got %s", acks.payloads[1].Result)
	}
	if acks.payloads[1].ErrorCode == "replay_duplicate" {
		t.Fatalf("unexpected replay_duplicate after transient apply failure")
	}
}

func TestGatewayBackend_ControllerHandleMessage_RemovesAndAcks(t *testing.T) {
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
		"policy_id": "77777777-7777-4777-8777-777777777777",
		"rule_type": "block",
		"action":    "remove",
	}
	ruleBytes, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}

	evt := &pb.PolicyUpdateEvent{
		PolicyId:         "77777777-7777-4777-8777-777777777777",
		Action:           "remove",
		RuleType:         "block",
		RuleData:         ruleBytes,
		RequiresAck:      true,
		Timestamp:        time.Now().Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 0,
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
		t.Fatalf("expected no apply on remove, got %d", delegate.applied)
	}
	if len(delegate.removed) != 1 || delegate.removed[0] != evt.PolicyId {
		t.Fatalf("expected remove for %s, got %#v", evt.PolicyId, delegate.removed)
	}
	if len(acks.payloads) != 1 {
		t.Fatalf("expected 1 ack payload, got %d", len(acks.payloads))
	}
	if acks.payloads[0].Result != ack.ResultApplied {
		t.Fatalf("expected applied ack, got %s", acks.payloads[0].Result)
	}
}

func TestGatewayBackend_ControllerHandleMessage_HoldsUntilApprovalThenApplies(t *testing.T) {
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
		"policy_id": "88888888-8888-4888-8888-888888888888",
		"rule_type": "block",
		"action":    "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds":       30,
			"dry_run":           true,
			"approval_required": true,
		},
	}
	ruleBytes, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}

	pendingEvt := &pb.PolicyUpdateEvent{
		PolicyId:         "88888888-8888-4888-8888-888888888888",
		Action:           "add",
		RuleType:         "block",
		RuleData:         ruleBytes,
		RequiresAck:      true,
		Timestamp:        time.Now().Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 10,
	}
	signPolicyEvent(t, priv, pub, pendingEvt)
	rawPending, err := proto.Marshal(pendingEvt)
	if err != nil {
		t.Fatalf("marshal pending evt: %v", err)
	}

	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: rawPending}); err != nil {
		t.Fatalf("handle pending: %v", err)
	}
	if delegate.applied != 0 {
		t.Fatalf("expected no apply before approval, got %d", delegate.applied)
	}
	if len(acks.payloads) != 0 {
		t.Fatalf("expected no ack before approval, got %d", len(acks.payloads))
	}
	if got := len(store.PendingApprovalKeys()); got != 1 {
		t.Fatalf("expected 1 pending approval stored, got %d", got)
	}

	approvalEvt := &pb.PolicyUpdateEvent{
		PolicyId:         "88888888-8888-4888-8888-888888888888",
		Action:           "approve",
		RuleType:         "block",
		RuleData:         ruleBytes,
		RequiresAck:      true,
		Timestamp:        time.Now().Add(2 * time.Second).Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 10,
	}
	signPolicyEvent(t, priv, pub, approvalEvt)
	rawApproval, err := proto.Marshal(approvalEvt)
	if err != nil {
		t.Fatalf("marshal approval evt: %v", err)
	}

	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: rawApproval}); err != nil {
		t.Fatalf("handle approval: %v", err)
	}
	if delegate.applied != 1 {
		t.Fatalf("expected apply after approval, got %d", delegate.applied)
	}
	if len(acks.payloads) != 1 {
		t.Fatalf("expected 1 ack after approval, got %d", len(acks.payloads))
	}
	if acks.payloads[0].Result != ack.ResultApplied {
		t.Fatalf("expected applied ack after approval, got %s", acks.payloads[0].Result)
	}
	if got := len(store.PendingApprovalKeys()); got != 0 {
		t.Fatalf("expected pending approval cleared after approval, got %d", got)
	}
}

func TestGatewayBackend_ControllerHandleMessage_RefreshesExistingPolicyOnRuleHashChange(t *testing.T) {
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

	buildEvent := func(ttl int64, protocol string) []byte {
		rule := map[string]any{
			"policy_id": "99999999-9999-4999-9999-999999999999",
			"rule_type": "block",
			"action":    "drop",
			"target": map[string]any{
				"cidrs":     []any{"198.51.100.0/24"},
				"direction": "egress",
				"scope":     "namespace",
				"protocols": []any{protocol},
				"selectors": map[string]any{"namespace": "cybermesh-gateway"},
			},
			"guardrails": map[string]any{
				"ttl_seconds": ttl,
				"dry_run":     true,
			},
		}
		ruleBytes, err := json.Marshal(rule)
		if err != nil {
			t.Fatalf("marshal rule: %v", err)
		}
		evt := &pb.PolicyUpdateEvent{
			PolicyId:         "99999999-9999-4999-9999-999999999999",
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
		return raw
	}

	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: buildEvent(30, "tcp")}); err != nil {
		t.Fatalf("handle initial: %v", err)
	}
	first, ok := store.Get("99999999-9999-4999-9999-999999999999")
	if !ok {
		t.Fatalf("expected stored policy after initial apply")
	}

	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: buildEvent(120, "udp")}); err != nil {
		t.Fatalf("handle refresh: %v", err)
	}
	refreshed, ok := store.Get("99999999-9999-4999-9999-999999999999")
	if !ok {
		t.Fatalf("expected stored policy after refresh apply")
	}
	if delegate.applied != 2 {
		t.Fatalf("expected 2 apply calls after refresh, got %d", delegate.applied)
	}
	if !refreshed.ExpiresAt.After(first.ExpiresAt) {
		t.Fatalf("expected refreshed expiry %s after initial %s", refreshed.ExpiresAt, first.ExpiresAt)
	}
	if len(acks.payloads) != 2 {
		t.Fatalf("expected 2 ack payloads, got %d", len(acks.payloads))
	}
	if acks.payloads[1].Reason != "applied_refresh_reapply" {
		t.Fatalf("expected refresh reapply ack reason, got %q", acks.payloads[1].Reason)
	}
}

func TestGatewayBackend_ControllerHandleMessage_RefreshOnlySkipsReapply(t *testing.T) {
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

	buildEvent := func(ttl int64) []byte {
		rule := map[string]any{
			"policy_id": "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
			"rule_type": "block",
			"action":    "drop",
			"target": map[string]any{
				"cidrs":     []any{"198.51.100.0/24"},
				"direction": "egress",
				"scope":     "namespace",
				"selectors": map[string]any{"namespace": "cybermesh-gateway"},
			},
			"guardrails": map[string]any{
				"ttl_seconds": ttl,
				"dry_run":     true,
			},
		}
		ruleBytes, err := json.Marshal(rule)
		if err != nil {
			t.Fatalf("marshal rule: %v", err)
		}
		evt := &pb.PolicyUpdateEvent{
			PolicyId:         "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
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
		return raw
	}

	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: buildEvent(30)}); err != nil {
		t.Fatalf("handle initial: %v", err)
	}
	first, ok := store.Get("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	if !ok {
		t.Fatalf("expected stored policy after initial apply")
	}

	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: buildEvent(120)}); err != nil {
		t.Fatalf("handle refresh-only: %v", err)
	}
	refreshed, ok := store.Get("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	if !ok {
		t.Fatalf("expected stored policy after refresh")
	}
	if delegate.applied != 1 {
		t.Fatalf("expected single apply call, got %d", delegate.applied)
	}
	if !refreshed.ExpiresAt.After(first.ExpiresAt) {
		t.Fatalf("expected refreshed expiry %s after initial %s", refreshed.ExpiresAt, first.ExpiresAt)
	}
	if len(acks.payloads) != 2 {
		t.Fatalf("expected 2 ack payloads, got %d", len(acks.payloads))
	}
	if acks.payloads[1].Reason != "applied_refresh" {
		t.Fatalf("expected refresh-only ack reason, got %q", acks.payloads[1].Reason)
	}
}

func TestGatewayBackend_ControllerHandleMessage_DurableOnlyDoesNotPromote(t *testing.T) {
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
		"policy_id": "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		"rule_type": "block",
		"action":    "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds":           30,
			"dry_run":               true,
			"fast_path_enabled":     true,
			"fast_path_ttl_seconds": 10,
		},
	}
	ruleBytes, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}
	evt := &pb.PolicyUpdateEvent{
		PolicyId:         "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
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

	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: raw}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(acks.payloads) != 1 {
		t.Fatalf("expected one ack, got %d", len(acks.payloads))
	}
	if acks.payloads[0].Reason != "applied_new" {
		t.Fatalf("expected applied_new reason, got %q", acks.payloads[0].Reason)
	}
	recState, ok := store.Get(evt.PolicyId)
	if !ok {
		t.Fatalf("expected stored state")
	}
	if recState.PendingConsensus || recState.Provisional || recState.PromotionState == "promoted" {
		t.Fatalf("expected durable-only state without promotion flags, got %#v", recState)
	}
}

func TestGatewayBackend_HandleFastEvent_AppliesProvisionallyAndExpires(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
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
	})

	spec, err := policy.ParseSpec("block", mustJSON(t, map[string]any{
		"schema_version": 1,
		"policy_id":      "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb",
		"rule_type":      "block",
		"action":         "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds":           30,
			"dry_run":               true,
			"fast_path_enabled":     true,
			"fast_path_ttl_seconds": 1,
		},
		"criteria":            map[string]any{"min_confidence": 0.95},
		"metadata":            map[string]any{"trace_id": "trace-fast-1"},
		"provisional":         true,
		"promotion_requested": true,
	}), 0, 0, time.Now().Unix())
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	evt := policy.Event{
		Fast: &policy.FastMitigationEnvelope{
			SchemaVersion: policy.FastMitigationDomain,
			MitigationID:  "11111111-1111-4111-8111-111111111111",
			PolicyID:      spec.ID,
			Action:        "drop",
			RuleType:      "block",
			Timestamp:     time.Now().Unix(),
			ContentHash:   mustContentHash(t, spec.Raw),
			Nonce:         hex.EncodeToString(make([]byte, 16)),
			Pubkey:        hex.EncodeToString(pub),
			ProducerID:    hex.EncodeToString(pub),
			Alg:           "Ed25519",
		},
		Spec: spec,
	}

	if err := ctrl.HandleFastEvent(context.Background(), evt); err != nil {
		t.Fatalf("HandleFastEvent: %v", err)
	}
	if delegate.applied != 1 {
		t.Fatalf("expected fast apply once, got %d", delegate.applied)
	}
	recState, ok := store.Get(spec.ID)
	if !ok || !recState.PendingConsensus {
		t.Fatalf("expected pending consensus record")
	}
	if len(acks.payloads) != 1 || !acks.payloads[0].FastPath {
		t.Fatalf("expected fast-path ack payload")
	}
	if acks.payloads[0].Reason != "applied_provisional" {
		t.Fatalf("expected provisional ack reason, got %q", acks.payloads[0].Reason)
	}

	expired := store.Expired(time.Now().UTC().Add(2 * time.Second))
	if len(expired) != 1 || expired[0].Reason != "fast_path_ttl" {
		t.Fatalf("expected fast_path_ttl expiry, got %#v", expired)
	}
}

func TestGatewayBackend_HandleFastEvent_PromotesOnDurablePolicyWithoutReapply(t *testing.T) {
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
	})

	spec, err := policy.ParseSpec("block", mustJSON(t, map[string]any{
		"schema_version": 1,
		"policy_id":      "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		"rule_type":      "block",
		"action":         "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds":           30,
			"dry_run":               true,
			"fast_path_enabled":     true,
			"fast_path_ttl_seconds": 10,
		},
		"provisional":         true,
		"promotion_requested": true,
	}), 0, 0, time.Now().Unix())
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	fastEvt := policy.Event{
		Fast: &policy.FastMitigationEnvelope{
			SchemaVersion: policy.FastMitigationDomain,
			MitigationID:  "22222222-2222-4222-8222-222222222222",
			PolicyID:      spec.ID,
			Action:        "drop",
			RuleType:      "block",
			Timestamp:     time.Now().Unix(),
			ContentHash:   mustContentHash(t, spec.Raw),
			Nonce:         hex.EncodeToString(make([]byte, 16)),
			Pubkey:        hex.EncodeToString(pub),
			ProducerID:    hex.EncodeToString(pub),
			Alg:           "Ed25519",
		},
		Spec: spec,
	}
	if err := ctrl.HandleFastEvent(context.Background(), fastEvt); err != nil {
		t.Fatalf("HandleFastEvent: %v", err)
	}

	durableEvt := &pb.PolicyUpdateEvent{
		PolicyId:         spec.ID,
		Action:           "add",
		RuleType:         "block",
		RuleData:         mustJSON(t, spec.Raw),
		RequiresAck:      true,
		Timestamp:        time.Now().Add(2 * time.Second).Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 10,
	}
	signPolicyEvent(t, priv, pub, durableEvt)
	raw, err := proto.Marshal(durableEvt)
	if err != nil {
		t.Fatalf("marshal evt: %v", err)
	}
	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: raw}); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	if delegate.applied != 1 {
		t.Fatalf("expected durable promotion to skip reapply, got %d applies", delegate.applied)
	}
	if len(acks.payloads) != 2 {
		t.Fatalf("expected two ack payloads, got %d", len(acks.payloads))
	}
	if acks.payloads[1].Reason != "applied_promoted" {
		t.Fatalf("expected promoted ack reason, got %q", acks.payloads[1].Reason)
	}
	recState, ok := store.Get(spec.ID)
	if !ok {
		t.Fatalf("expected promoted state record")
	}
	if recState.PendingConsensus || recState.Provisional || recState.PromotionState != "promoted" {
		t.Fatalf("unexpected promotion state: %#v", recState)
	}
}

func TestGatewayBackend_HandleMessage_RemoveRevokesPendingProvisional(t *testing.T) {
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
	})

	spec, err := policy.ParseSpec("block", mustJSON(t, map[string]any{
		"schema_version": 1,
		"policy_id":      "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
		"rule_type":      "block",
		"action":         "drop",
		"target": map[string]any{
			"cidrs":     []any{"198.51.100.0/24"},
			"direction": "egress",
			"scope":     "namespace",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds":           30,
			"dry_run":               true,
			"fast_path_enabled":     true,
			"fast_path_ttl_seconds": 10,
		},
		"provisional":         true,
		"promotion_requested": true,
	}), 0, 0, time.Now().Unix())
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	fastEvt := policy.Event{
		Fast: &policy.FastMitigationEnvelope{
			SchemaVersion: policy.FastMitigationDomain,
			MitigationID:  "33333333-3333-4333-8333-333333333333",
			PolicyID:      spec.ID,
			Action:        "drop",
			RuleType:      "block",
			Timestamp:     time.Now().Unix(),
			ContentHash:   mustContentHash(t, spec.Raw),
			Nonce:         hex.EncodeToString(make([]byte, 16)),
			Pubkey:        hex.EncodeToString(pub),
			ProducerID:    hex.EncodeToString(pub),
			Alg:           "Ed25519",
		},
		Spec: spec,
	}
	if err := ctrl.HandleFastEvent(context.Background(), fastEvt); err != nil {
		t.Fatalf("HandleFastEvent: %v", err)
	}

	removeEvt := &pb.PolicyUpdateEvent{
		PolicyId:         spec.ID,
		Action:           "remove",
		RuleType:         "block",
		RuleData:         mustJSON(t, map[string]any{"schema_version": 1, "policy_id": spec.ID, "rule_type": "block", "action": "remove"}),
		RequiresAck:      true,
		Timestamp:        time.Now().Add(2 * time.Second).Unix(),
		EffectiveHeight:  1,
		ExpirationHeight: 0,
	}
	signPolicyEvent(t, priv, pub, removeEvt)
	raw, err := proto.Marshal(removeEvt)
	if err != nil {
		t.Fatalf("marshal evt: %v", err)
	}
	if err := ctrl.HandleMessage(context.Background(), &sarama.ConsumerMessage{Value: raw}); err != nil {
		t.Fatalf("HandleMessage remove: %v", err)
	}
	if len(delegate.removed) != 1 {
		t.Fatalf("expected one remove call, got %#v", delegate.removed)
	}
	if len(acks.payloads) != 2 {
		t.Fatalf("expected two ack payloads, got %d", len(acks.payloads))
	}
	if acks.payloads[1].Reason != "provisional_revoked" {
		t.Fatalf("expected provisional_revoked reason, got %q", acks.payloads[1].Reason)
	}
	if _, ok := store.Get(spec.ID); ok {
		t.Fatalf("expected provisional state to be removed")
	}
}

func TestGatewayBackend_HandleFastEvent_RejectsSecondRenewalWhilePendingConsensus(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
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
	})

	spec, err := policy.ParseSpec("block", mustJSON(t, map[string]any{
		"schema_version": 1,
		"policy_id":      "ffffffff-ffff-4fff-8fff-ffffffffffff",
		"rule_type":      "block",
		"action":         "drop",
		"target": map[string]any{
			"ips":       []any{"203.0.113.5"},
			"scope":     "namespace",
			"direction": "egress",
			"selectors": map[string]any{"namespace": "cybermesh-gateway"},
		},
		"guardrails": map[string]any{
			"ttl_seconds":           30,
			"dry_run":               true,
			"fast_path_enabled":     true,
			"fast_path_ttl_seconds": 10,
		},
		"metadata": map[string]any{
			"trace_id":   "trace-fast-renew",
			"anomaly_id": "11111111-1111-4111-8111-111111111111",
		},
		"provisional":         true,
		"promotion_requested": true,
	}), 0, 0, time.Now().Unix())
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	buildEvent := func(mitigationID string, ts int64) policy.Event {
		return policy.Event{
			Fast: &policy.FastMitigationEnvelope{
				SchemaVersion: policy.FastMitigationDomain,
				MitigationID:  mitigationID,
				PolicyID:      spec.ID,
				Action:        "drop",
				RuleType:      "block",
				Timestamp:     ts,
				ContentHash:   mustContentHash(t, spec.Raw),
				Nonce:         hex.EncodeToString([]byte{byte(ts), 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}),
				Pubkey:        hex.EncodeToString(pub),
				ProducerID:    hex.EncodeToString(pub),
				Alg:           "Ed25519",
			},
			Spec: spec,
		}
	}

	if err := ctrl.HandleFastEvent(context.Background(), buildEvent("44444444-4444-4444-8444-444444444444", time.Now().Unix())); err != nil {
		t.Fatalf("first HandleFastEvent: %v", err)
	}
	if err := ctrl.HandleFastEvent(context.Background(), buildEvent("55555555-5555-4555-8555-555555555555", time.Now().Add(2*time.Second).Unix())); err != nil {
		t.Fatalf("second HandleFastEvent: %v", err)
	}
	if err := ctrl.HandleFastEvent(context.Background(), buildEvent("66666666-6666-4666-8666-666666666666", time.Now().Add(4*time.Second).Unix())); err != nil {
		t.Fatalf("third HandleFastEvent: %v", err)
	}

	if delegate.applied != 1 {
		t.Fatalf("expected only initial fast apply, got %d", delegate.applied)
	}
	if len(acks.payloads) != 3 {
		t.Fatalf("expected 3 ack payloads, got %d", len(acks.payloads))
	}
	if acks.payloads[1].Reason != "applied_provisional_refresh" {
		t.Fatalf("expected first renewal ack, got %q", acks.payloads[1].Reason)
	}
	if acks.payloads[2].ErrorCode != "fast_renewal_exceeded" {
		t.Fatalf("expected fast_renewal_exceeded, got %q", acks.payloads[2].ErrorCode)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return b
}

func mustContentHash(t *testing.T, v any) string {
	t.Helper()
	sum := sha256.Sum256(mustJSON(t, v))
	return hex.EncodeToString(sum[:])
}
