package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

const (
	defaultHistoryRetention = 10 * time.Minute
	defaultLockTimeout      = 3 * time.Second
)

const (
	globalScope          = "global"
	tenantScopePrefix    = "tenant:"
	regionScopePrefix    = "region:"
	namespaceScopePrefix = "namespace:"
	nodeScopePrefix      = "node:"
	clusterScopeKey      = "cluster"
	snapshotVersion      = 5
)

// Record tracks a policy along with metadata for TTL management.
type Record struct {
	Spec             policy.PolicySpec `json:"spec"`
	AppliedAt        time.Time         `json:"applied_at"`
	ExpiresAt        time.Time         `json:"expires_at"`
	GuardrailTTL     time.Duration     `json:"guardrail_ttl"`
	PreConsensus     time.Time         `json:"pre_consensus_deadline"`
	Rollback         time.Time         `json:"rollback_deadline"`
	FastPathDeadline time.Time         `json:"fast_path_deadline"`
	PendingConsensus bool              `json:"pending_consensus"`
	Reason           string            `json:"-"`
}

// Store maintains applied policies for reconciliation and persistence.
type Store struct {
	mu               sync.RWMutex
	records          map[string]Record
	persistPath      string
	lockPath         string
	history          map[string][]time.Time
	pending          map[string][]byte
	historyRetention time.Duration
	checksumEnabled  bool
	lockTimeout      time.Duration
	lastApplied      map[string]time.Time
}

// Options configure Store construction.
type Options struct {
	PersistPath      string
	HistoryRetention time.Duration
	EnableChecksum   bool
	LockTimeout      time.Duration
}

// NewStore constructs a store optionally backed by on-disk persistence.
func NewStore(opts Options) *Store {
	path := strings.TrimSpace(opts.PersistPath)
	retention := opts.HistoryRetention
	if retention <= 0 {
		retention = defaultHistoryRetention
	}
	lockTimeout := opts.LockTimeout
	if lockTimeout <= 0 {
		lockTimeout = defaultLockTimeout
	}

	lockPath := ""
	if path != "" {
		lockPath = path + ".lock"
	}

	return &Store{
		records:          make(map[string]Record),
		persistPath:      path,
		lockPath:         lockPath,
		history:          make(map[string][]time.Time),
		pending:          make(map[string][]byte),
		lastApplied:      make(map[string]time.Time),
		historyRetention: retention,
		checksumEnabled:  opts.EnableChecksum,
		lockTimeout:      lockTimeout,
	}
}

// Load restores previously persisted records.
func (s *Store) Load() error {
	if s.persistPath == "" {
		return nil
	}
	data, err := os.ReadFile(s.persistPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("state store: read %s: %w", s.persistPath, err)
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("state store: parse snapshot: %w", err)
	}
	if snap.Checksum != "" {
		computed, err := computeChecksum(snap)
		if err != nil {
			return fmt.Errorf("state store: checksum compute: %w", err)
		}
		if !strings.EqualFold(computed, snap.Checksum) {
			return fmt.Errorf("state store: checksum mismatch")
		}
	}
	loaded := make(map[string]Record, len(snap.Records))
	lastApplied := make(map[string]time.Time)
	for _, rec := range snap.Records {
		rec.GuardrailTTL = time.Duration(rec.Spec.Guardrails.TTLSeconds) * time.Second
		if rec.GuardrailTTL > 0 && rec.AppliedAt.IsZero() {
			rec.AppliedAt = time.Now().UTC()
		}
		if rec.GuardrailTTL > 0 {
			rec.ExpiresAt = rec.AppliedAt.Add(rec.GuardrailTTL)
		} else {
			rec.ExpiresAt = time.Time{}
		}
		if rec.Spec.Guardrails.PreConsensusTTLSeconds != nil {
			rec.PreConsensus = rec.AppliedAt.Add(time.Duration(*rec.Spec.Guardrails.PreConsensusTTLSeconds) * time.Second)
		}
		if rec.Spec.Guardrails.RollbackIfNoCommitAfter != nil {
			rec.Rollback = rec.AppliedAt.Add(time.Duration(*rec.Spec.Guardrails.RollbackIfNoCommitAfter) * time.Second)
		}
		if rec.Spec.ID == "" {
			continue
		}
		loaded[rec.Spec.ID] = rec
		lastApplied[rec.Spec.ID] = rec.AppliedAt
	}
	now := time.Now().UTC()
	histories := make(map[string][]time.Time)
	retention := s.historyRetention
	if len(snap.RateHistory) > 0 {
		for key, entries := range snap.RateHistory {
			histories[key] = pruneHistory(entries, now, retention)
		}
	} else {
		histories[globalScope] = pruneHistory(snap.History, now, retention)
	}
	if _, ok := histories[globalScope]; !ok {
		histories[globalScope] = []time.Time{}
	}
	s.mu.Lock()
	s.records = loaded
	s.history = histories
	if len(snap.PendingApprovals) > 0 {
		s.pending = snap.PendingApprovals
	} else {
		s.pending = make(map[string][]byte)
	}
	if len(snap.LastApplied) > 0 {
		clone := make(map[string]time.Time, len(snap.LastApplied))
		for id, ts := range snap.LastApplied {
			clone[id] = ts
		}
		s.lastApplied = clone
	} else {
		s.lastApplied = lastApplied
	}
	s.mu.Unlock()
	return nil
}

// Upsert inserts or updates a policy record and persists state if configured.
func (s *Store) Upsert(spec policy.PolicySpec, appliedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	expires := appliedAt
	if spec.Guardrails.TTLSeconds > 0 {
		expires = appliedAt.Add(time.Duration(spec.Guardrails.TTLSeconds) * time.Second)
	}

	rec := Record{
		Spec:         spec,
		AppliedAt:    appliedAt,
		ExpiresAt:    expires,
		GuardrailTTL: time.Duration(spec.Guardrails.TTLSeconds) * time.Second,
		PreConsensus: deadline(appliedAt, spec.Guardrails.PreConsensusTTLSeconds),
		Rollback:     deadline(appliedAt, spec.Guardrails.RollbackIfNoCommitAfter),
	}
	if spec.Guardrails.FastPathTTLSeconds != nil {
		rec.FastPathDeadline = appliedAt.Add(time.Duration(*spec.Guardrails.FastPathTTLSeconds) * time.Second)
	}
	rec.PendingConsensus = false
	s.records[spec.ID] = rec

	s.recordHistoryLocked(globalScope, appliedAt)
	if tenant := normalizedTenant(spec); tenant != "" {
		s.recordHistoryLocked(tenantScopePrefix+tenant, appliedAt)
	}
	if region := normalizedRegion(spec); region != "" {
		s.recordHistoryLocked(regionScopePrefix+region, appliedAt)
	}
	if ns := normalizedNamespace(spec); ns != "" {
		s.recordHistoryLocked(namespaceScopePrefix+ns, appliedAt)
	}
	if strings.EqualFold(spec.Target.Scope, "cluster") {
		s.recordHistoryLocked(clusterScopeKey, appliedAt)
	}
	if node := normalizedNode(spec); node != "" {
		s.recordHistoryLocked(nodeScopePrefix+node, appliedAt)
	}
	s.lastApplied[spec.ID] = appliedAt

	return s.persistLocked()
}

// Remove deletes a policy by ID and persists the resulting state.
func (s *Store) Remove(policyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, policyID)
	return s.persistLocked()
}

// MarkPendingConsensus sets pending status for a fast-path policy.
func (s *Store) MarkPendingConsensus(policyID string, deadline time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[policyID]
	if !ok {
		return fmt.Errorf("state store: mark pending consensus: policy %s not found", policyID)
	}
	rec.PendingConsensus = true
	if !deadline.IsZero() {
		rec.FastPathDeadline = deadline
	}
	s.records[policyID] = rec
	return s.persistLocked()
}

// ClearPendingConsensus removes pending state for policy.
func (s *Store) ClearPendingConsensus(policyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[policyID]
	if !ok {
		return nil
	}
	rec.PendingConsensus = false
	rec.FastPathDeadline = time.Time{}
	s.records[policyID] = rec
	return s.persistLocked()
}

// PendingFastPath returns policies awaiting consensus.
func (s *Store) PendingFastPath() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var pending []Record
	for _, rec := range s.records {
		if rec.PendingConsensus {
			pending = append(pending, rec)
		}
	}
	return pending
}

// Get returns record if present.
func (s *Store) Get(policyID string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[policyID]
	return rec, ok
}

// List returns a copy of all records.
func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		res = append(res, rec)
	}
	return res
}

// SavePendingApproval stores serialized policy event requiring approval.
func (s *Store) SavePendingApproval(key string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(payload) == 0 {
		delete(s.pending, key)
	} else {
		s.pending[key] = append([]byte(nil), payload...)
	}
	return s.persistLocked()
}

// PendingApproval retrieves serialized approval payload.
func (s *Store) PendingApproval(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	payload, ok := s.pending[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), payload...), true
}

// RemovePendingApproval deletes stored pending approval entry.
func (s *Store) RemovePendingApproval(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, key)
	return s.persistLocked()
}

// PendingApprovalKeys returns all stored approval identifiers.
func (s *Store) PendingApprovalKeys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.pending))
	for k := range s.pending {
		keys = append(keys, k)
	}
	return keys
}

// LastAppliedWithin reports whether the policy was applied within the provided window.
func (s *Store) LastAppliedWithin(policyID string, window time.Duration, now time.Time) bool {
	if window <= 0 {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	last, ok := s.lastApplied[policyID]
	if !ok {
		return false
	}
	return now.Sub(last) < window
}

// ReconcileLedger aligns the in-memory store with policies provided by ledger snapshot.
// Returns removed records and newly added specs, allowing callers to adjust enforcement backends.
func (s *Store) ReconcileLedger(ledger map[string]policy.PolicySpec, appliedAt time.Time) ([]Record, []policy.PolicySpec, error) {
	if ledger == nil {
		return nil, nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	removed := make([]Record, 0)
	for id, rec := range s.records {
		if _, ok := ledger[id]; ok {
			continue
		}
		removed = append(removed, rec)
		delete(s.records, id)
	}

	added := make([]policy.PolicySpec, 0)
	for id, spec := range ledger {
		if spec.ID == "" {
			continue
		}
		if _, exists := s.records[id]; exists {
			continue
		}

		expires := appliedAt
		if spec.Guardrails.TTLSeconds > 0 {
			expires = appliedAt.Add(time.Duration(spec.Guardrails.TTLSeconds) * time.Second)
		} else {
			expires = time.Time{}
		}

		rec := Record{
			Spec:         spec,
			AppliedAt:    appliedAt,
			ExpiresAt:    expires,
			GuardrailTTL: time.Duration(spec.Guardrails.TTLSeconds) * time.Second,
			PreConsensus: deadline(appliedAt, spec.Guardrails.PreConsensusTTLSeconds),
			Rollback:     deadline(appliedAt, spec.Guardrails.RollbackIfNoCommitAfter),
		}
		s.records[id] = rec
		added = append(added, spec)

		s.recordHistoryLocked(globalScope, appliedAt)
		if tenant := normalizedTenant(spec); tenant != "" {
			s.recordHistoryLocked(tenantScopePrefix+tenant, appliedAt)
		}
		if region := normalizedRegion(spec); region != "" {
			s.recordHistoryLocked(regionScopePrefix+region, appliedAt)
		}
		if ns := normalizedNamespace(spec); ns != "" {
			s.recordHistoryLocked(namespaceScopePrefix+ns, appliedAt)
		}
		if strings.EqualFold(spec.Target.Scope, "cluster") {
			s.recordHistoryLocked(clusterScopeKey, appliedAt)
		}
		if node := normalizedNode(spec); node != "" {
			s.recordHistoryLocked(nodeScopePrefix+node, appliedAt)
		}
		s.lastApplied[spec.ID] = appliedAt
	}

	if len(removed) == 0 && len(added) == 0 {
		return nil, nil, nil
	}

	if err := s.persistLocked(); err != nil {
		return nil, nil, err
	}
	return removed, added, nil
}

// ActiveCount returns number of active policies.
func (s *Store) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// RecentCount returns number of policies applied within window ending at now.
func (s *Store) RecentCount(window time.Duration, now time.Time) int {
	return s.RecentCountFor(globalScope, window, now)
}

// RecentCountFor returns number of policies applied for scope within window.
func (s *Store) RecentCountFor(scope string, window time.Duration, now time.Time) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := s.history[scope]
	if window <= 0 {
		return len(entries)
	}
	threshold := now.Add(-window)
	count := 0
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Before(threshold) {
			break
		}
		count++
	}
	return count
}

// Expired returns records whose TTL has elapsed at the provided time.
func (s *Store) Expired(now time.Time) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var expired []Record
	for _, rec := range s.records {
		if !rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt) {
			rec.Reason = "ttl_expired"
			expired = append(expired, rec)
			continue
		}
		if !rec.PreConsensus.IsZero() && now.After(rec.PreConsensus) {
			rec.Reason = "pre_consensus_ttl"
			expired = append(expired, rec)
			continue
		}
		if !rec.Rollback.IsZero() && now.After(rec.Rollback) {
			rec.Reason = "rollback_timeout"
			expired = append(expired, rec)
		}
	}
	return expired
}

// Snapshot returns a serialisable copy of the state.
func (s *Store) Snapshot() map[string]Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		copy[k] = v
	}
	return copy
}

func (s *Store) recordHistoryLocked(scope string, timestamp time.Time) {
	if s.history == nil {
		s.history = make(map[string][]time.Time)
	}
	list := append(s.history[scope], timestamp)
	s.history[scope] = pruneHistory(list, timestamp, s.historyRetention)
}

func normalizedTenant(spec policy.PolicySpec) string {
	if spec.Target.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Tenant))
	}
	if spec.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Tenant))
	}
	return ""
}

func normalizedRegion(spec policy.PolicySpec) string {
	if spec.Target.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Region))
	}
	if spec.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Region))
	}
	return ""
}

func normalizedNamespace(spec policy.PolicySpec) string {
	if spec.Target.Namespace != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Namespace))
	}
	if ns, ok := spec.Target.Selectors["namespace"]; ok {
		return strings.ToLower(strings.TrimSpace(ns))
	}
	return ""
}

func normalizedNode(spec policy.PolicySpec) string {
	if node, ok := spec.Target.Selectors["node"]; ok {
		return strings.ToLower(strings.TrimSpace(node))
	}
	return ""
}

func (s *Store) persistLocked() error {
	if s.persistPath == "" {
		return nil
	}

	unlock, err := s.acquireLock()
	if err != nil {
		return err
	}
	defer unlock()

	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, rec)
	}
	histCopy := make(map[string][]time.Time, len(s.history))
	for key, entries := range s.history {
		histCopy[key] = append([]time.Time(nil), entries...)
	}
	pendingCopy := make(map[string][]byte, len(s.pending))
	for key, payload := range s.pending {
		pendingCopy[key] = append([]byte(nil), payload...)
	}
	lastAppliedCopy := make(map[string]time.Time, len(s.lastApplied))
	for id, ts := range s.lastApplied {
		lastAppliedCopy[id] = ts
	}

	snap := snapshot{
		Version:          snapshotVersion,
		Records:          records,
		History:          append([]time.Time(nil), histCopy[globalScope]...),
		RateHistory:      histCopy,
		PendingApprovals: pendingCopy,
		LastApplied:      lastAppliedCopy,
		CreatedAt:        time.Now().UTC(),
	}
	if s.checksumEnabled {
		checksum, err := computeChecksum(snap)
		if err != nil {
			return fmt.Errorf("state store: checksum compute: %w", err)
		}
		snap.Checksum = checksum
	}
	bytes, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("state store: marshal snapshot: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.persistPath), 0o755); err != nil {
		return fmt.Errorf("state store: ensure dir: %w", err)
	}
	tmp := s.persistPath + ".tmp"
	if err := os.WriteFile(tmp, bytes, 0o600); err != nil {
		return fmt.Errorf("state store: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.persistPath); err != nil {
		return fmt.Errorf("state store: rename snapshot: %w", err)
	}
	return nil
}

type snapshot struct {
	Version          int                    `json:"version"`
	Records          []Record               `json:"records"`
	History          []time.Time            `json:"history,omitempty"`
	RateHistory      map[string][]time.Time `json:"rate_history,omitempty"`
	PendingApprovals map[string][]byte      `json:"pending_approvals,omitempty"`
	LastApplied      map[string]time.Time   `json:"last_applied,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	Checksum         string                 `json:"checksum,omitempty"`
}

func computeChecksum(s snapshot) (string, error) {
	clone := s
	clone.Checksum = ""
	bytes, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

func deadline(appliedAt time.Time, seconds *int64) time.Time {
	if seconds == nil || *seconds <= 0 {
		return time.Time{}
	}
	return appliedAt.Add(time.Duration(*seconds) * time.Second)
}

func (s *Store) acquireLock() (func(), error) {
	if s.lockPath == "" {
		return func() {}, nil
	}
	deadline := time.Now().Add(s.lockTimeout)
	for {
		file, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return func() {
				_ = file.Close()
				_ = os.Remove(s.lockPath)
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("state store: lock file: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("state store: lock timeout after %s", s.lockTimeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func pruneHistory(history []time.Time, now time.Time, horizon time.Duration) []time.Time {
	if horizon <= 0 || len(history) == 0 {
		return history
	}
	threshold := now.Add(-horizon)
	start := len(history)
	for i, ts := range history {
		if !ts.Before(threshold) {
			start = i
			break
		}
	}
	if start == len(history) {
		return []time.Time{}
	}
	return append([]time.Time(nil), history[start:]...)
}

// NextPreConsensusDeadline returns the soonest pre-consensus deadline after now.
func (s *Store) NextPreConsensusDeadline(now time.Time) (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var soonest time.Time
	for _, rec := range s.records {
		if rec.PreConsensus.IsZero() {
			continue
		}
		if !now.Before(rec.PreConsensus) {
			continue
		}
		if soonest.IsZero() || rec.PreConsensus.Before(soonest) {
			soonest = rec.PreConsensus
		}
	}
	if soonest.IsZero() {
		return time.Time{}, false
	}
	return soonest, true
}
