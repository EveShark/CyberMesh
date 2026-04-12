package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"backend/pkg/config"
	"backend/pkg/utils"
)

const (
	tupleSourceAccessMemberships = "auth_access_memberships"
	tupleSourceSupportDelegation = "support_delegations"
	maxTupleAuditKeys            = 20
)

type openFGATupleManager struct {
	baseURL   string
	storeID   string
	token     string
	client    *http.Client
	interval  time.Duration
	batchSize int
	logger    *utils.Logger
}

type managedTuple struct {
	User     string
	Relation string
	Object   string
}

type openFGAWriteRequest struct {
	Writes  *openFGAWriteTupleGroup `json:"writes,omitempty"`
	Deletes *openFGAWriteTupleGroup `json:"deletes,omitempty"`
}

type openFGAWriteTupleGroup struct {
	TupleKeys []openFGATupleKey `json:"tuple_keys,omitempty"`
}

func newOpenFGATupleManager(cfg *config.APIConfig, logger *utils.Logger) *openFGATupleManager {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.OpenFGAAPIURL), "/")
	storeID := strings.TrimSpace(cfg.OpenFGAStoreID)
	if baseURL == "" || storeID == "" {
		return nil
	}
	timeout := cfg.OpenFGATimeout
	if timeout <= 0 {
		timeout = 750 * time.Millisecond
	}
	interval := cfg.OpenFGATupleReconcileInterval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	batchSize := cfg.OpenFGATupleBatchSize
	if batchSize <= 0 {
		batchSize = 200
	}
	return &openFGATupleManager{
		baseURL:   baseURL,
		storeID:   storeID,
		token:     strings.TrimSpace(cfg.OpenFGAToken),
		client:    &http.Client{Timeout: timeout},
		interval:  interval,
		batchSize: batchSize,
		logger:    logger,
	}
}

func (s *Server) runOpenFGATupleReconciler() {
	defer s.wg.Done()
	runOnce := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*s.tupleManager.interval)
		defer cancel()
		if err := s.reconcileOpenFGATuples(ctx); err != nil && s.logger != nil {
			s.logger.Warn("openfga tuple reconciliation failed",
				utils.ZapString("error", err.Error()))
		}
	}
	runOnce()
	ticker := time.NewTicker(s.tupleManager.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

func (s *Server) reconcileOpenFGATuples(ctx context.Context) error {
	if s.tupleManager == nil {
		return nil
	}
	db, err := s.getDB()
	if err != nil || db == nil {
		return nil
	}

	sources := []string{tupleSourceAccessMemberships, tupleSourceSupportDelegation}
	desiredBySource := make(map[string]map[string]managedTuple, len(sources))
	for _, source := range sources {
		desired, err := s.loadDesiredTuplesBySource(ctx, db, source)
		if err != nil {
			return err
		}
		desiredBySource[source] = desired
	}

	totalWrites := 0
	totalDeletes := 0
	for _, source := range sources {
		desired := desiredBySource[source]
		current, err := loadManagedTuples(ctx, db, source)
		if err != nil {
			return err
		}

		toWrite, toDelete := diffManagedTuples(desired, current)
		toDelete = filterDeletesOwnedByOtherSources(toDelete, source, desiredBySource)
		if len(toWrite) == 0 && len(toDelete) == 0 {
			continue
		}
		if err := s.tupleManager.applyTupleDiff(ctx, toWrite, toDelete); err != nil {
			return err
		}
		if err := persistManagedTupleDiff(ctx, db, source, toWrite, toDelete); err != nil {
			return err
		}
		s.emitTupleAudit("reconcile", "", source, toWrite, toDelete)
		totalWrites += len(toWrite)
		totalDeletes += len(toDelete)
	}
	if totalWrites == 0 && totalDeletes == 0 {
		return nil
	}
	if s.logger != nil {
		s.logger.Info("openfga tuple reconciliation applied",
			utils.ZapInt("writes", totalWrites),
			utils.ZapInt("deletes", totalDeletes))
	}
	return nil
}

func (s *Server) queuePrincipalTupleSync(principalID string) {
	s.queuePrincipalTupleSyncWithMode(principalID, false)
}

func (s *Server) queuePrincipalTupleSyncImmediate(principalID string) {
	s.queuePrincipalTupleSyncWithMode(principalID, true)
}

func (s *Server) queuePrincipalTupleSyncWithMode(principalID string, immediate bool) {
	if s == nil || s.tupleManager == nil {
		return
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return
	}
	minInterval := 5 * time.Second
	if s.tupleManager.interval > 0 && s.tupleManager.interval < minInterval {
		minInterval = s.tupleManager.interval
	}
	if !immediate {
		if lastValue, ok := s.tupleSyncLastQueued.Load(principalID); ok {
			if lastQueuedAt, ok := lastValue.(time.Time); ok && time.Since(lastQueuedAt) < minInterval {
				return
			}
		}
	}
	s.tupleSyncLastQueued.Store(principalID, time.Now())
	if _, loaded := s.tupleSyncInFlight.LoadOrStore(principalID, struct{}{}); loaded {
		return
	}
	go func() {
		defer s.tupleSyncInFlight.Delete(principalID)
		timeout := 2 * s.tupleManager.client.Timeout
		if timeout <= 0 {
			timeout = 1500 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := s.syncOpenFGATuplesForPrincipal(ctx, principalID); err != nil && s.logger != nil {
			s.logger.Warn("openfga principal tuple sync failed",
				utils.ZapString("principal_id", principalID),
				utils.ZapString("error", err.Error()))
		}
	}()
}

func (s *Server) syncOpenFGATuplesForPrincipal(ctx context.Context, principalID string) error {
	if s == nil || s.tupleManager == nil {
		return nil
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return nil
	}
	db, err := s.getDB()
	if err != nil {
		return err
	}
	sources := []string{tupleSourceAccessMemberships, tupleSourceSupportDelegation}
	desiredBySource := make(map[string]map[string]managedTuple, len(sources))
	for _, source := range sources {
		desired, err := s.loadDesiredTuplesForPrincipalBySource(ctx, db, source, principalID)
		if err != nil {
			return err
		}
		desiredBySource[source] = desired
	}
	for _, source := range sources {
		desired := desiredBySource[source]
		current, err := loadManagedTuplesForPrincipal(ctx, db, source, principalID)
		if err != nil {
			return err
		}
		toWrite, toDelete := diffManagedTuples(desired, current)
		toDelete = filterDeletesOwnedByOtherSources(toDelete, source, desiredBySource)
		if len(toWrite) == 0 && len(toDelete) == 0 {
			continue
		}
		if err := s.tupleManager.applyTupleDiff(ctx, toWrite, toDelete); err != nil {
			return err
		}
		if err := persistManagedTupleDiff(ctx, db, source, toWrite, toDelete); err != nil {
			return err
		}
		s.emitTupleAudit("principal_sync", principalID, source, toWrite, toDelete)
	}
	return nil
}

func (s *Server) emitTupleAudit(mode, principalID, source string, toWrite, toDelete []managedTuple) {
	if s == nil || s.audit == nil || (len(toWrite) == 0 && len(toDelete) == 0) {
		return
	}
	fields := map[string]interface{}{
		"sync_mode":     strings.TrimSpace(mode),
		"principal_id":  strings.TrimSpace(principalID),
		"tuple_source":  strings.TrimSpace(source),
		"writes_count":  len(toWrite),
		"deletes_count": len(toDelete),
		"write_tuples":  summarizeTupleAuditKeys(toWrite),
		"delete_tuples": summarizeTupleAuditKeys(toDelete),
	}
	if err := s.audit.Security("security.openfga_tuple_sync", fields); err != nil && s.logger != nil {
		s.logger.Warn("failed to emit openfga tuple audit",
			utils.ZapString("source", source),
			utils.ZapString("error", err.Error()))
	}
}

func summarizeTupleAuditKeys(values []managedTuple) []string {
	if len(values) == 0 {
		return nil
	}
	limit := len(values)
	if limit > maxTupleAuditKeys {
		limit = maxTupleAuditKeys
	}
	keys := make([]string, 0, limit)
	for _, tuple := range values[:limit] {
		keys = append(keys, tupleKey(tuple))
	}
	return keys
}

func (s *Server) loadDesiredTuplesBySource(ctx context.Context, db *sql.DB, source string) (map[string]managedTuple, error) {
	switch source {
	case tupleSourceAccessMemberships:
		return loadDesiredMembershipTuples(ctx, db)
	case tupleSourceSupportDelegation:
		return loadDesiredDelegationTuples(ctx, db, s != nil && s.config != nil && s.config.BreakGlassEnabled)
	default:
		return nil, fmt.Errorf("unknown tuple source %q", source)
	}
}

func (s *Server) loadDesiredTuplesForPrincipalBySource(ctx context.Context, db *sql.DB, source, principalID string) (map[string]managedTuple, error) {
	switch source {
	case tupleSourceAccessMemberships:
		return loadDesiredMembershipTuplesForPrincipal(ctx, db, principalID)
	case tupleSourceSupportDelegation:
		return loadDesiredDelegationTuplesForPrincipal(ctx, db, principalID, s != nil && s.config != nil && s.config.BreakGlassEnabled)
	default:
		return nil, fmt.Errorf("unknown tuple source %q", source)
	}
}

func loadDesiredMembershipTuples(ctx context.Context, db *sql.DB) (map[string]managedTuple, error) {
	const query = `
		SELECT principal_id, access_id, role
		FROM auth_access_memberships
		WHERE status = 'active'
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	desired := make(map[string]managedTuple)
	for rows.Next() {
		var principalID, accessID, role string
		if err := rows.Scan(&principalID, &accessID, &role); err != nil {
			return nil, err
		}
		principalID = strings.TrimSpace(principalID)
		accessID = strings.TrimSpace(accessID)
		role = strings.TrimSpace(strings.ToLower(role))
		if principalID == "" || accessID == "" || role == "" || role == "support_delegate" {
			continue
		}
		tuple := managedTuple{
			User:     principalID,
			Relation: role,
			Object:   openFGAObject("access", accessID),
		}
		desired[tupleKey(tuple)] = tuple
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return desired, nil
}

func loadDesiredMembershipTuplesForPrincipal(ctx context.Context, db *sql.DB, principalID string) (map[string]managedTuple, error) {
	const query = `
		SELECT principal_id, access_id, role
		FROM auth_access_memberships
		WHERE principal_id = $1
		  AND status = 'active'
	`
	rows, err := db.QueryContext(ctx, query, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	desired := make(map[string]managedTuple)
	for rows.Next() {
		var user, accessID, role string
		if err := rows.Scan(&user, &accessID, &role); err != nil {
			return nil, err
		}
		user = strings.TrimSpace(user)
		accessID = strings.TrimSpace(accessID)
		role = strings.TrimSpace(strings.ToLower(role))
		if user == "" || accessID == "" || role == "" || role == "support_delegate" {
			continue
		}
		tuple := managedTuple{
			User:     user,
			Relation: role,
			Object:   openFGAObject("access", accessID),
		}
		desired[tupleKey(tuple)] = tuple
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return desired, nil
}

func loadDesiredDelegationTuples(ctx context.Context, db *sql.DB, includeBreakGlass bool) (map[string]managedTuple, error) {
	const query = `
		SELECT sd.principal_id, sda.access_id, sd.break_glass, sd.approval_reference
		FROM support_delegations sd
		JOIN support_delegation_accesses sda ON sda.delegation_id = sd.delegation_id
		WHERE sd.status = 'active'
		  AND sd.starts_at <= now()
		  AND sd.expires_at > now()
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDesiredDelegationTuples(rows, includeBreakGlass)
}

func loadDesiredDelegationTuplesForPrincipal(ctx context.Context, db *sql.DB, principalID string, includeBreakGlass bool) (map[string]managedTuple, error) {
	const query = `
		SELECT sd.principal_id, sda.access_id, sd.break_glass, sd.approval_reference
		FROM support_delegations sd
		JOIN support_delegation_accesses sda ON sda.delegation_id = sd.delegation_id
		WHERE sd.principal_id = $1
		  AND sd.status = 'active'
		  AND sd.starts_at <= now()
		  AND sd.expires_at > now()
	`
	rows, err := db.QueryContext(ctx, query, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDesiredDelegationTuples(rows, includeBreakGlass)
}

func scanDesiredDelegationTuples(rows *sql.Rows, includeBreakGlass bool) (map[string]managedTuple, error) {
	desired := make(map[string]managedTuple)
	for rows.Next() {
		var principalID, accessID string
		var breakGlass bool
		var approvalReference string
		if err := rows.Scan(&principalID, &accessID, &breakGlass, &approvalReference); err != nil {
			return nil, err
		}
		principalID = strings.TrimSpace(principalID)
		accessID = strings.TrimSpace(accessID)
		approvalReference = strings.TrimSpace(approvalReference)
		if principalID == "" || accessID == "" || (breakGlass && (!includeBreakGlass || approvalReference == "")) {
			continue
		}
		tuple := managedTuple{
			User:     principalID,
			Relation: "support_delegate",
			Object:   openFGAObject("access", accessID),
		}
		desired[tupleKey(tuple)] = tuple
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return desired, nil
}

func loadManagedTuples(ctx context.Context, db *sql.DB, source string) (map[string]managedTuple, error) {
	const query = `
		SELECT user_key, relation, object_key
		FROM openfga_managed_tuples
		WHERE source = $1
	`
	rows, err := db.QueryContext(ctx, query, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	current := make(map[string]managedTuple)
	for rows.Next() {
		var user, relation, object string
		if err := rows.Scan(&user, &relation, &object); err != nil {
			return nil, err
		}
		tuple := managedTuple{
			User:     strings.TrimSpace(user),
			Relation: strings.TrimSpace(relation),
			Object:   strings.TrimSpace(object),
		}
		if tuple.User == "" || tuple.Relation == "" || tuple.Object == "" {
			continue
		}
		current[tupleKey(tuple)] = tuple
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return current, nil
}

func loadManagedTuplesForPrincipal(ctx context.Context, db *sql.DB, source, principalID string) (map[string]managedTuple, error) {
	const query = `
		SELECT user_key, relation, object_key
		FROM openfga_managed_tuples
		WHERE source = $1 AND user_key = $2
	`
	rows, err := db.QueryContext(ctx, query, source, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	current := make(map[string]managedTuple)
	for rows.Next() {
		var user, relation, object string
		if err := rows.Scan(&user, &relation, &object); err != nil {
			return nil, err
		}
		tuple := managedTuple{
			User:     strings.TrimSpace(user),
			Relation: strings.TrimSpace(relation),
			Object:   strings.TrimSpace(object),
		}
		if tuple.User == "" || tuple.Relation == "" || tuple.Object == "" {
			continue
		}
		current[tupleKey(tuple)] = tuple
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return current, nil
}

func diffManagedTuples(desired, current map[string]managedTuple) ([]managedTuple, []managedTuple) {
	toWrite := make([]managedTuple, 0)
	toDelete := make([]managedTuple, 0)
	for key, tuple := range desired {
		if _, ok := current[key]; !ok {
			toWrite = append(toWrite, tuple)
		}
	}
	for key, tuple := range current {
		if _, ok := desired[key]; !ok {
			toDelete = append(toDelete, tuple)
		}
	}
	return toWrite, toDelete
}

func filterDeletesOwnedByOtherSources(toDelete []managedTuple, source string, desiredBySource map[string]map[string]managedTuple) []managedTuple {
	if len(toDelete) == 0 {
		return nil
	}
	filtered := make([]managedTuple, 0, len(toDelete))
	for _, tuple := range toDelete {
		key := tupleKey(tuple)
		stillOwned := false
		for otherSource, desired := range desiredBySource {
			if otherSource == source {
				continue
			}
			if _, ok := desired[key]; ok {
				stillOwned = true
				break
			}
		}
		if !stillOwned {
			filtered = append(filtered, tuple)
		}
	}
	return filtered
}

func tupleKey(tuple managedTuple) string {
	return tuple.User + "|" + tuple.Relation + "|" + tuple.Object
}

func (m *openFGATupleManager) applyTupleDiff(ctx context.Context, toWrite, toDelete []managedTuple) error {
	writeBatches := toTupleBatches(toWrite, m.batchSize)
	deleteBatches := toTupleBatches(toDelete, m.batchSize)

	maxBatches := len(writeBatches)
	if len(deleteBatches) > maxBatches {
		maxBatches = len(deleteBatches)
	}
	for i := 0; i < maxBatches; i++ {
		var writes, deletes []openFGATupleKey
		if i < len(writeBatches) {
			writes = writeBatches[i]
		}
		if i < len(deleteBatches) {
			deletes = deleteBatches[i]
		}
		if err := m.writeBatch(ctx, writes, deletes); err != nil {
			return err
		}
	}
	return nil
}

func toTupleBatches(values []managedTuple, batchSize int) [][]openFGATupleKey {
	if len(values) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	batches := make([][]openFGATupleKey, 0, (len(values)+batchSize-1)/batchSize)
	for start := 0; start < len(values); start += batchSize {
		end := start + batchSize
		if end > len(values) {
			end = len(values)
		}
		chunk := make([]openFGATupleKey, 0, end-start)
		for _, tuple := range values[start:end] {
			chunk = append(chunk, openFGATupleKey{
				User:     tuple.User,
				Relation: tuple.Relation,
				Object:   tuple.Object,
			})
		}
		batches = append(batches, chunk)
	}
	return batches
}

func (m *openFGATupleManager) writeBatch(ctx context.Context, writes, deletes []openFGATupleKey) error {
	body := openFGAWriteRequest{}
	if len(writes) > 0 {
		body.Writes = &openFGAWriteTupleGroup{TupleKeys: writes}
	}
	if len(deletes) > 0 {
		body.Deletes = &openFGAWriteTupleGroup{TupleKeys: deletes}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/stores/%s/write", m.baseURL, m.storeID), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("openfga write returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func persistManagedTupleDiff(ctx context.Context, db *sql.DB, source string, toWrite, toDelete []managedTuple) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, tuple := range toWrite {
		if _, err := tx.ExecContext(ctx, `
			UPSERT INTO openfga_managed_tuples (source, user_key, relation, object_key, updated_at)
			VALUES ($1, $2, $3, $4, now())
		`, source, tuple.User, tuple.Relation, tuple.Object); err != nil {
			return err
		}
	}
	for _, tuple := range toDelete {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM openfga_managed_tuples
			WHERE source = $1 AND user_key = $2 AND relation = $3 AND object_key = $4
		`, source, tuple.User, tuple.Relation, tuple.Object); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
