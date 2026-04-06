package wiring

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"backend/pkg/ingest/kafka"
	"backend/pkg/state"
)

type lifecycleIntentState struct {
	TsMs          int64
	Action        string
	ControlAction string
	SeenAt        time.Time
	Hash          [32]byte
	HasHash       bool
}

type lifecycleEvictionPlan struct {
	Hashes [][32]byte
	SeenAt time.Time
}

func lifecyclePlanKey(env *state.Envelope) string {
	if env == nil {
		return ""
	}
	return hex.EncodeToString(env.ProducerID) + ":" + hex.EncodeToString(env.Nonce) + ":" + hex.EncodeToString(env.ContentHash[:])
}

func (s *Service) enforceLifecyclePreAdmit(topic string, tx state.Transaction) error {
	if s == nil || tx == nil || !strings.EqualFold(strings.TrimSpace(topic), "ai.policy.v1") {
		return nil
	}
	action, controlAction, rollbackID, key, tsMs := extractLifecycleIntent(tx.Payload())
	if !isLifecycleIntent(action, controlAction, rollbackID) {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(s.lifecycleMode))
	auditOnly := false
	switch mode {
	case "off":
		// Off mode bypasses lifecycle gate entirely (pre-lifecycle behavior).
		atomic.AddUint64(&s.lifecyclePreAdmitAllowed, 1)
		return nil
	case "audit":
		auditOnly = true
	}
	if !s.lifecycleRollbackEnabled && isRollbackIntent(action, controlAction, rollbackID) {
		atomic.AddUint64(&s.lifecycleCompactionRejected, 1)
		if auditOnly {
			atomic.AddUint64(&s.lifecyclePreAdmitAllowed, 1)
			return nil
		}
		return kafka.NewPreAdmitRejected("lifecycle_rollback_disabled", nil)
	}
	if key == "" {
		if auditOnly {
			atomic.AddUint64(&s.lifecycleCompactionRejected, 1)
			atomic.AddUint64(&s.lifecyclePreAdmitAllowed, 1)
			return nil
		}
		atomic.AddUint64(&s.lifecycleCompactionRejected, 1)
		return kafka.NewPreAdmitRejected("lifecycle_policy_id_missing", nil)
	}
	if auditOnly {
		// Audit mode observes compaction decisions but never blocks admission.
		if _, reason, reject := s.compactionDecision(key, tsMs, action, controlAction, tx.Envelope()); reject {
			atomic.AddUint64(&s.lifecycleCompactionRejected, 1)
			if reason == "lifecycle_compact_superseded" {
				atomic.AddUint64(&s.lifecycleCompactionSuperseded, 1)
			}
		}
		atomic.AddUint64(&s.lifecyclePreAdmitAllowed, 1)
		return nil
	}
	evicted, reason, reject := s.compactionDecision(key, tsMs, action, controlAction, tx.Envelope())
	if reject {
		atomic.AddUint64(&s.lifecycleCompactionRejected, 1)
		if reason == "lifecycle_compact_superseded" {
			atomic.AddUint64(&s.lifecycleCompactionSuperseded, 1)
		}
		return kafka.NewPreAdmitRejected(reason, nil)
	}
	if len(evicted) > 0 {
		s.stageLifecyclePostAdmitEvictions(tx.Envelope(), evicted)
	}
	atomic.AddUint64(&s.lifecyclePreAdmitAllowed, 1)
	return nil
}

func extractLifecycleIntent(payload []byte) (string, string, string, string, int64) {
	if len(payload) == 0 {
		return "", "", "", "", 0
	}
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return "", "", "", "", 0
	}
	lookup := func(m map[string]any, key string) string {
		if m == nil {
			return ""
		}
		v, ok := m[key]
		if !ok {
			return ""
		}
		if s, ok := v.(string); ok {
			return strings.ToLower(strings.TrimSpace(s))
		}
		return ""
	}
	action := lookup(root, "action")
	controlAction := lookup(root, "control_action")
	rollbackID := strings.TrimSpace(lookup(root, "rollback_policy_id"))
	policyID := strings.TrimSpace(lookup(root, "policy_id"))
	scope := strings.TrimSpace(lookup(root, "scope"))
	tenant := strings.TrimSpace(lookup(root, "tenant"))
	region := strings.TrimSpace(lookup(root, "region"))
	tsMs := asInt64(root["ts_ms"])
	if tsMs <= 0 {
		tsMs = asInt64(root["source_event_ts_ms"])
	}
	if nested, ok := root["params"].(map[string]any); ok {
		if action == "" {
			action = lookup(nested, "action")
		}
		if controlAction == "" {
			controlAction = lookup(nested, "control_action")
		}
		if rollbackID == "" {
			rollbackID = strings.TrimSpace(lookup(nested, "rollback_policy_id"))
		}
		if policyID == "" {
			policyID = strings.TrimSpace(lookup(nested, "policy_id"))
		}
		if scope == "" {
			scope = strings.TrimSpace(lookup(nested, "scope"))
		}
		if tenant == "" {
			tenant = strings.TrimSpace(lookup(nested, "tenant"))
		}
		if region == "" {
			region = strings.TrimSpace(lookup(nested, "region"))
		}
		if tsMs <= 0 {
			tsMs = asInt64(nested["ts_ms"])
		}
	}
	if metadata, ok := root["metadata"].(map[string]any); ok {
		if action == "" {
			action = lookup(metadata, "action")
		}
		if controlAction == "" {
			controlAction = lookup(metadata, "control_action")
		}
		if rollbackID == "" {
			rollbackID = strings.TrimSpace(lookup(metadata, "rollback_policy_id"))
		}
		if policyID == "" {
			policyID = strings.TrimSpace(lookup(metadata, "policy_id"))
		}
		if scope == "" {
			scope = strings.TrimSpace(lookup(metadata, "scope"))
		}
		if tenant == "" {
			tenant = strings.TrimSpace(lookup(metadata, "tenant"))
		}
		if region == "" {
			region = strings.TrimSpace(lookup(metadata, "region"))
		}
		if tsMs <= 0 {
			tsMs = asInt64(metadata["ts_ms"])
		}
	}
	key := lifecycleEntityKey(policyID, scope, tenant, region)
	return action, controlAction, rollbackID, key, tsMs
}

func isRollbackIntent(action, controlAction, rollbackID string) bool {
	if strings.TrimSpace(rollbackID) != "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(action), "rollback") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(controlAction), "rollback")
}

func isLifecycleIntent(action, controlAction, rollbackID string) bool {
	if isRollbackIntent(action, controlAction, rollbackID) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "add", "update", "remove", "approve", "reject", "refresh", "promote", "suppress", "bypass":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(controlAction)) {
	case "add", "update", "remove", "approve", "reject", "refresh", "promote", "suppress", "bypass":
		return true
	}
	return false
}

func lifecycleEntityKey(policyID, scope, tenant, region string) string {
	policyID = strings.TrimSpace(strings.ToLower(policyID))
	if policyID == "" {
		return ""
	}
	return policyID + "|" + strings.TrimSpace(strings.ToLower(scope)) + "|" + strings.TrimSpace(strings.ToLower(tenant)) + "|" + strings.TrimSpace(strings.ToLower(region))
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func (s *Service) compactionDecision(key string, tsMs int64, action, controlAction string, env *state.Envelope) ([][32]byte, string, bool) {
	if s == nil || s.lifecycleCompactionWindow <= 0 || key == "" {
		return nil, "", false
	}
	now := time.Now()
	if tsMs <= 0 {
		tsMs = now.UnixMilli()
	}
	currentHash := [32]byte{}
	currentHasHash := false
	if env != nil {
		currentHash = env.ContentHash
		currentHasHash = true
	}
	normalizedAction := strings.ToLower(strings.TrimSpace(action))
	normalizedControl := strings.ToLower(strings.TrimSpace(controlAction))
	s.lifecycleCompactionMu.Lock()
	defer s.lifecycleCompactionMu.Unlock()
	if s.lifecycleCompactionSweepInterval <= 0 {
		s.lifecycleCompactionSweepInterval = 5 * time.Second
	}
	if s.lifecycleCompactionLastSweep.IsZero() || now.Sub(s.lifecycleCompactionLastSweep) >= s.lifecycleCompactionSweepInterval {
		for k, st := range s.lifecycleCompactionState {
			if now.Sub(st.SeenAt) > s.lifecycleCompactionWindow {
				delete(s.lifecycleCompactionState, k)
			}
		}
		s.lifecycleCompactionLastSweep = now
	}
	prev, ok := s.lifecycleCompactionState[key]
	if ok {
		if tsMs < prev.TsMs {
			return nil, "lifecycle_compact_stale", true
		}
		if tsMs == prev.TsMs && normalizedAction == prev.Action && normalizedControl == prev.ControlAction {
			return nil, "lifecycle_compact_duplicate", true
		}
		if prev.Action == "remove" || prev.ControlAction == "remove" {
			switch normalizedAction {
			case "refresh", "update", "add", "approve", "promote", "suppress", "bypass":
				return nil, "lifecycle_compact_superseded", true
			}
		}
	}
	evicted := make([][32]byte, 0, 1)
	if ok && prev.HasHash && (!currentHasHash || prev.Hash != currentHash) {
		evicted = append(evicted, prev.Hash)
	}
	s.lifecycleCompactionState[key] = lifecycleIntentState{
		TsMs:          tsMs,
		Action:        normalizedAction,
		ControlAction: normalizedControl,
		SeenAt:        now,
		Hash:          currentHash,
		HasHash:       currentHasHash,
	}
	return evicted, "", false
}

func (s *Service) stageLifecyclePostAdmitEvictions(env *state.Envelope, hashes [][32]byte) {
	if s == nil || env == nil || len(hashes) == 0 {
		return
	}
	s.lifecycleCompactionMu.Lock()
	defer s.lifecycleCompactionMu.Unlock()
	now := time.Now()
	key := lifecyclePlanKey(env)
	if key == "" {
		return
	}
	s.lifecyclePostAdmitEvictions[key] = lifecycleEvictionPlan{
		Hashes: append([][32]byte(nil), hashes...),
		SeenAt: now,
	}
	ttl := s.lifecycleCompactionWindow
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	for k, v := range s.lifecyclePostAdmitEvictions {
		if now.Sub(v.SeenAt) > ttl {
			delete(s.lifecyclePostAdmitEvictions, k)
		}
	}
}

func (s *Service) consumeLifecyclePostAdmitEvictions(env *state.Envelope) [][32]byte {
	if s == nil || env == nil {
		return nil
	}
	s.lifecycleCompactionMu.Lock()
	defer s.lifecycleCompactionMu.Unlock()
	key := lifecyclePlanKey(env)
	if key == "" {
		return nil
	}
	plan, ok := s.lifecyclePostAdmitEvictions[key]
	if !ok {
		return nil
	}
	delete(s.lifecyclePostAdmitEvictions, key)
	return append([][32]byte(nil), plan.Hashes...)
}

func (s *Service) applyLifecyclePostAdmit(topic string, tx state.Transaction) {
	if s == nil || tx == nil || !strings.EqualFold(strings.TrimSpace(topic), "ai.policy.v1") {
		return
	}
	if !isLifecycleTransaction(tx) {
		return
	}
	env := tx.Envelope()
	if env == nil {
		return
	}
	atomic.AddUint64(&s.lifecycleMempoolAddSuccess, 1)
	hashes := s.consumeLifecyclePostAdmitEvictions(env)
	if len(hashes) == 0 || s.mp == nil {
		return
	}
	s.mp.Remove(hashes...)
	atomic.AddUint64(&s.lifecyclePostAdmitEvicted, uint64(len(hashes)))
}

func (s *Service) handleLifecycleAdmitFailed(topic string, tx state.Transaction) {
	if s == nil || tx == nil || !strings.EqualFold(strings.TrimSpace(topic), "ai.policy.v1") {
		return
	}
	if !isLifecycleTransaction(tx) {
		return
	}
	env := tx.Envelope()
	if env == nil {
		return
	}
	atomic.AddUint64(&s.lifecycleMempoolAddFailure, 1)
	_ = s.consumeLifecyclePostAdmitEvictions(env)
}

func isLifecycleTransaction(tx state.Transaction) bool {
	if tx == nil || tx.Type() != state.TxPolicy {
		return false
	}
	action, controlAction, rollbackID, _, _ := extractLifecycleIntent(tx.Payload())
	return isLifecycleIntent(action, controlAction, rollbackID)
}
