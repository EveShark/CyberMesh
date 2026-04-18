package policystate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type queryExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type schemaSupport struct {
	enabled bool
}

type outboxProjectionRow struct {
	BlockHeight     int64
	Payload         []byte
	Status          string
	WorkflowID      sql.NullString
	TraceID         sql.NullString
	SourceEventID   sql.NullString
	SentinelEventID sql.NullString
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ackProjectionRow struct {
	Tenant          sql.NullString
	Result          sql.NullString
	Reason          sql.NullString
	WorkflowID      sql.NullString
	TraceID         sql.NullString
	SourceEventID   sql.NullString
	SentinelEventID sql.NullString
	ObservedAt      time.Time
}

type policyProjection struct {
	PolicyID        string
	PolicyType      string
	Target          string
	Rules           []byte
	Active          bool
	Priority        int
	CreatedHeight   int64
	CreatedAt       time.Time
	UpdatedHeight   int64
	UpdatedAt       time.Time
	WorkflowID      string
	Tenant          string
	CurrentStatus   string
	LatestAction    string
	LatestAckResult string
	LatestAckReason string
	TraceID         string
	AnomalyID       string
	SourceEventID   string
	SentinelEventID string
}

var schemaCache sync.Map // map[*sql.DB]schemaSupport

func Prime(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	_, err := loadSchemaSupport(ctx, db, db)
	return err
}

func Refresh(ctx context.Context, db *sql.DB, query queryExecer, policyID string) error {
	policyID = strings.TrimSpace(policyID)
	if query == nil || policyID == "" || db == nil {
		return nil
	}
	cached, ok := schemaCache.Load(db)
	if !ok || !cached.(schemaSupport).enabled {
		return nil
	}

	latestOutbox, ok, err := loadLatestOutbox(ctx, query, policyID)
	if err != nil {
		return fmt.Errorf("policy state refresh: latest outbox: %w", err)
	}
	latestAck, _, err := loadLatestAck(ctx, query, policyID)
	if err != nil {
		return fmt.Errorf("policy state refresh: latest ack: %w", err)
	}
	if !ok {
		if latestAck.ObservedAt.IsZero() {
			return nil
		}
		projection := buildProjection(policyID, outboxProjectionRow{}, 0, time.Time{}, latestAck)
		return upsertProjection(ctx, query, projection)
	}

	firstHeight, firstCreatedAt, err := loadFirstOutbox(ctx, query, policyID)
	if err != nil {
		return fmt.Errorf("policy state refresh: first outbox: %w", err)
	}

	projection := buildProjection(policyID, latestOutbox, firstHeight, firstCreatedAt, latestAck)
	return upsertProjection(ctx, query, projection)
}

func RefreshMany(ctx context.Context, db *sql.DB, query queryExecer, policyIDs []string) error {
	if query == nil || db == nil {
		return nil
	}
	ids := dedupePolicyIDs(policyIDs)
	if len(ids) == 0 {
		return nil
	}
	cached, ok := schemaCache.Load(db)
	if !ok || !cached.(schemaSupport).enabled {
		return nil
	}

	latestOutboxByPolicy, err := loadLatestOutboxMany(ctx, query, ids)
	if err != nil {
		return fmt.Errorf("policy state refresh batch: latest outbox: %w", err)
	}
	latestAckByPolicy, err := loadLatestAckMany(ctx, query, ids)
	if err != nil {
		return fmt.Errorf("policy state refresh batch: latest ack: %w", err)
	}
	firstOutboxByPolicy, err := loadFirstOutboxMany(ctx, query, ids)
	if err != nil {
		return fmt.Errorf("policy state refresh batch: first outbox: %w", err)
	}

	for _, policyID := range ids {
		latestOutbox, hasOutbox := latestOutboxByPolicy[policyID]
		latestAck, hasAck := latestAckByPolicy[policyID]
		if !hasOutbox && !hasAck {
			continue
		}

		firstHeight := int64(0)
		firstCreatedAt := time.Time{}
		if first, ok := firstOutboxByPolicy[policyID]; ok {
			firstHeight = first.BlockHeight
			firstCreatedAt = first.CreatedAt
		}

		projection := buildProjection(policyID, latestOutbox, firstHeight, firstCreatedAt, latestAck)
		if err := upsertProjection(ctx, query, projection); err != nil {
			return fmt.Errorf("policy state refresh batch: upsert projection: %w", err)
		}
	}
	return nil
}

func loadSchemaSupport(ctx context.Context, db *sql.DB, query queryExecer) (schemaSupport, error) {
	if db != nil {
		if cached, ok := schemaCache.Load(db); ok {
			return cached.(schemaSupport), nil
		}
	}
	var source interface {
		QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	}
	if db != nil {
		source = db
	} else {
		source = query
	}
	if source == nil {
		return schemaSupport{}, nil
	}
	schemaProbeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	schema := schemaSupport{}
	var reg sql.NullString
	if err := source.QueryRowContext(schemaProbeCtx, `SELECT to_regclass('state_policies')::STRING`).Scan(&reg); err != nil {
		return schema, err
	}
	if !reg.Valid || strings.TrimSpace(reg.String) == "" {
		if db != nil {
			schemaCache.Store(db, schema)
		}
		return schema, nil
	}
	const requiredColumns = 11
	var count int
	if err := source.QueryRowContext(schemaProbeCtx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'state_policies'
		  AND column_name IN (
			'policy_id_text',
			'workflow_id',
			'tenant',
			'current_status',
			'latest_action',
			'latest_ack_result',
			'latest_ack_reason',
			'trace_id',
			'anomaly_id',
			'source_event_id',
			'sentinel_event_id'
		  )
	`).Scan(&count); err != nil {
		return schema, err
	}
	schema.enabled = count >= requiredColumns
	if db != nil {
		schemaCache.Store(db, schema)
	}
	return schema, nil
}

func loadLatestOutbox(ctx context.Context, query queryExecer, policyID string) (outboxProjectionRow, bool, error) {
	var row outboxProjectionRow
	err := query.QueryRowContext(ctx, `
		SELECT block_height, payload, status,
		       workflow_id, trace_id, source_event_id, sentinel_event_id,
		       created_at, updated_at
		FROM control_policy_outbox
		WHERE policy_id = $1
		ORDER BY created_at DESC, tx_index DESC
		LIMIT 1
	`, policyID).Scan(
		&row.BlockHeight, &row.Payload, &row.Status,
		&row.WorkflowID, &row.TraceID, &row.SourceEventID, &row.SentinelEventID,
		&row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return outboxProjectionRow{}, false, nil
	}
	if err != nil {
		return outboxProjectionRow{}, false, err
	}
	return row, true, nil
}

func loadFirstOutbox(ctx context.Context, query queryExecer, policyID string) (int64, time.Time, error) {
	var blockHeight int64
	var createdAt time.Time
	err := query.QueryRowContext(ctx, `
		SELECT block_height, created_at
		FROM control_policy_outbox
		WHERE policy_id = $1
		ORDER BY created_at ASC, tx_index ASC
		LIMIT 1
	`, policyID).Scan(&blockHeight, &createdAt)
	if err == sql.ErrNoRows {
		return 0, time.Time{}, nil
	}
	return blockHeight, createdAt, err
}

func loadLatestOutboxMany(ctx context.Context, query queryExecer, policyIDs []string) (map[string]outboxProjectionRow, error) {
	out := make(map[string]outboxProjectionRow, len(policyIDs))
	if len(policyIDs) == 0 {
		return out, nil
	}
	placeholders, args := stringPlaceholders(policyIDs)
	rows, err := query.QueryContext(ctx, `
		SELECT policy_id, block_height, payload, status,
		       workflow_id, trace_id, source_event_id, sentinel_event_id,
		       created_at, updated_at
		FROM (
			SELECT policy_id, block_height, payload, status,
			       workflow_id, trace_id, source_event_id, sentinel_event_id,
			       created_at, updated_at, tx_index,
			       ROW_NUMBER() OVER (PARTITION BY policy_id ORDER BY created_at DESC, tx_index DESC) AS rn
			FROM control_policy_outbox
			WHERE policy_id IN (`+placeholders+`)
		) ranked
		WHERE rn = 1
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var policyID string
		var row outboxProjectionRow
		if err := rows.Scan(
			&policyID, &row.BlockHeight, &row.Payload, &row.Status,
			&row.WorkflowID, &row.TraceID, &row.SourceEventID, &row.SentinelEventID,
			&row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		policyID = strings.TrimSpace(policyID)
		if policyID != "" {
			out[policyID] = row
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func loadFirstOutboxMany(ctx context.Context, query queryExecer, policyIDs []string) (map[string]outboxProjectionRow, error) {
	out := make(map[string]outboxProjectionRow, len(policyIDs))
	if len(policyIDs) == 0 {
		return out, nil
	}
	placeholders, args := stringPlaceholders(policyIDs)
	rows, err := query.QueryContext(ctx, `
		SELECT policy_id, block_height, created_at
		FROM (
			SELECT policy_id, block_height, created_at, tx_index,
			       ROW_NUMBER() OVER (PARTITION BY policy_id ORDER BY created_at ASC, tx_index ASC) AS rn
			FROM control_policy_outbox
			WHERE policy_id IN (`+placeholders+`)
		) ranked
		WHERE rn = 1
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var policyID string
		var row outboxProjectionRow
		if err := rows.Scan(&policyID, &row.BlockHeight, &row.CreatedAt); err != nil {
			return nil, err
		}
		policyID = strings.TrimSpace(policyID)
		if policyID != "" {
			out[policyID] = row
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func loadLatestAck(ctx context.Context, query queryExecer, policyID string) (ackProjectionRow, bool, error) {
	var row ackProjectionRow
	err := query.QueryRowContext(ctx, `
		SELECT tenant, result, reason, workflow_id, trace_id, source_event_id, sentinel_event_id, observed_at
		FROM policy_acks
		WHERE policy_id = $1
		ORDER BY observed_at DESC
		LIMIT 1
	`, policyID).Scan(
		&row.Tenant, &row.Result, &row.Reason, &row.WorkflowID, &row.TraceID,
		&row.SourceEventID, &row.SentinelEventID, &row.ObservedAt,
	)
	if err == sql.ErrNoRows {
		return ackProjectionRow{}, false, nil
	}
	if err != nil {
		return ackProjectionRow{}, false, err
	}
	return row, true, nil
}

func loadLatestAckMany(ctx context.Context, query queryExecer, policyIDs []string) (map[string]ackProjectionRow, error) {
	out := make(map[string]ackProjectionRow, len(policyIDs))
	if len(policyIDs) == 0 {
		return out, nil
	}
	placeholders, args := stringPlaceholders(policyIDs)
	rows, err := query.QueryContext(ctx, `
		SELECT policy_id, tenant, result, reason, workflow_id, trace_id, source_event_id, sentinel_event_id, observed_at
		FROM (
			SELECT policy_id, tenant, result, reason, workflow_id, trace_id, source_event_id, sentinel_event_id, observed_at,
			       ROW_NUMBER() OVER (PARTITION BY policy_id ORDER BY observed_at DESC) AS rn
			FROM policy_acks
			WHERE policy_id IN (`+placeholders+`)
		) ranked
		WHERE rn = 1
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var policyID string
		var row ackProjectionRow
		if err := rows.Scan(
			&policyID, &row.Tenant, &row.Result, &row.Reason,
			&row.WorkflowID, &row.TraceID, &row.SourceEventID, &row.SentinelEventID, &row.ObservedAt,
		); err != nil {
			return nil, err
		}
		policyID = strings.TrimSpace(policyID)
		if policyID != "" {
			out[policyID] = row
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func dedupePolicyIDs(policyIDs []string) []string {
	if len(policyIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(policyIDs))
	seen := make(map[string]struct{}, len(policyIDs))
	for _, id := range policyIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func stringPlaceholders(values []string) (string, []any) {
	parts := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for i, v := range values {
		parts = append(parts, "$"+strconv.Itoa(i+1))
		args = append(args, v)
	}
	return strings.Join(parts, ", "), args
}

func buildProjection(policyID string, latestOutbox outboxProjectionRow, firstHeight int64, firstCreatedAt time.Time, latestAck ackProjectionRow) policyProjection {
	raw := map[string]any{}
	if len(latestOutbox.Payload) > 0 {
		_ = json.Unmarshal(latestOutbox.Payload, &raw)
	}
	if firstHeight == 0 {
		firstHeight = latestOutbox.BlockHeight
	}
	if firstCreatedAt.IsZero() {
		firstCreatedAt = latestOutbox.CreatedAt
	}
	if firstCreatedAt.IsZero() {
		firstCreatedAt = latestAck.ObservedAt
	}

	updatedAt := latestOutbox.UpdatedAt
	if latestAck.ObservedAt.After(updatedAt) {
		updatedAt = latestAck.ObservedAt
	}
	if updatedAt.IsZero() {
		updatedAt = latestAck.ObservedAt
	}
	workflowID := firstNonEmpty(
		nullString(latestAck.WorkflowID),
		nullString(latestOutbox.WorkflowID),
		nestedString(raw, "trace", "workflow_id"),
		nestedString(raw, "metadata", "workflow_id"),
		extractString(raw, "workflow_id"),
	)
	traceID := firstNonEmpty(
		nullString(latestAck.TraceID),
		nullString(latestOutbox.TraceID),
		nestedString(raw, "trace", "id"),
		nestedString(raw, "metadata", "trace_id"),
		extractString(raw, "trace_id"),
	)
	sourceEventID := firstNonEmpty(
		nullString(latestAck.SourceEventID),
		nullString(latestOutbox.SourceEventID),
		nestedString(raw, "metadata", "source_event_id"),
		extractString(raw, "source_event_id"),
	)
	sentinelEventID := firstNonEmpty(
		nullString(latestAck.SentinelEventID),
		nullString(latestOutbox.SentinelEventID),
		nestedString(raw, "metadata", "sentinel_event_id"),
		extractString(raw, "sentinel_event_id"),
	)
	latestAction := firstNonEmpty(
		extractString(raw, "control_action"),
		extractString(raw, "action"),
		nestedString(raw, "trace", "control_action"),
	)
	latestAckResult := nullString(latestAck.Result)
	latestAckReason := nullString(latestAck.Reason)
	tenant := firstNonEmpty(
		nullString(latestAck.Tenant),
		extractString(raw, "tenant"),
		nestedString(raw, "metadata", "tenant"),
		nestedString(raw, "trace", "tenant"),
		nestedString(raw, "target", "tenant"),
		nestedString(raw, "target", "tenant_id"),
	)

	currentStatus := strings.TrimSpace(latestOutbox.Status)
	if currentStatus == "" {
		currentStatus = deriveAckOnlyStatus(latestAckResult)
	}

	return policyProjection{
		PolicyID:        policyID,
		PolicyType:      derivePolicyType(raw, latestAction),
		Target:          deriveTarget(raw),
		Rules:           normalizeRules(latestOutbox.Payload),
		Active:          deriveActive(latestOutbox.Status, latestAction, latestAckResult),
		Priority:        derivePriority(raw),
		CreatedHeight:   firstHeight,
		CreatedAt:       firstCreatedAt,
		UpdatedHeight:   latestOutbox.BlockHeight,
		UpdatedAt:       updatedAt,
		WorkflowID:      workflowID,
		Tenant:          tenant,
		CurrentStatus:   currentStatus,
		LatestAction:    latestAction,
		LatestAckResult: latestAckResult,
		LatestAckReason: latestAckReason,
		TraceID:         traceID,
		AnomalyID: firstNonEmpty(
			extractString(raw, "anomaly_id"),
			nestedString(raw, "metadata", "anomaly_id"),
		),
		SourceEventID:   sourceEventID,
		SentinelEventID: sentinelEventID,
	}
}

func deriveAckOnlyStatus(latestAckResult string) string {
	switch strings.ToLower(strings.TrimSpace(latestAckResult)) {
	case "applied", "success", "acked":
		return "acked"
	case "failed", "rejected":
		return "terminal_failed"
	default:
		return ""
	}
}

func upsertProjection(ctx context.Context, query queryExecer, row policyProjection) error {
	_, err := query.ExecContext(ctx, `
		INSERT INTO state_policies (
			policy_id, policy_id_text, policy_type, target, rules, active, priority,
			created_height, created_at, updated_height, updated_at,
			workflow_id, tenant, current_status, latest_action, latest_ack_result, latest_ack_reason,
			trace_id, anomaly_id, source_event_id, sentinel_event_id
		) VALUES (
			$1, NULLIF($2, ''), $3, $4, $5, $6, $7,
			$8, $9, $10, $11,
			NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''),
			NULLIF($18, ''), NULLIF($19, ''), NULLIF($20, ''), NULLIF($21, '')
		)
		ON CONFLICT (policy_id) DO UPDATE SET
			policy_id_text = EXCLUDED.policy_id_text,
			policy_type = EXCLUDED.policy_type,
			target = EXCLUDED.target,
			rules = EXCLUDED.rules,
			active = EXCLUDED.active,
			priority = EXCLUDED.priority,
			updated_height = EXCLUDED.updated_height,
			updated_at = EXCLUDED.updated_at,
			workflow_id = EXCLUDED.workflow_id,
			tenant = EXCLUDED.tenant,
			current_status = EXCLUDED.current_status,
			latest_action = EXCLUDED.latest_action,
			latest_ack_result = EXCLUDED.latest_ack_result,
			latest_ack_reason = EXCLUDED.latest_ack_reason,
			trace_id = EXCLUDED.trace_id,
			anomaly_id = EXCLUDED.anomaly_id,
			source_event_id = EXCLUDED.source_event_id,
			sentinel_event_id = EXCLUDED.sentinel_event_id
	`, []byte(row.PolicyID), row.PolicyID, row.PolicyType, row.Target, row.Rules, row.Active, row.Priority,
		row.CreatedHeight, row.CreatedAt, row.UpdatedHeight, row.UpdatedAt,
		row.WorkflowID, row.Tenant, row.CurrentStatus, row.LatestAction, row.LatestAckResult, row.LatestAckReason,
		row.TraceID, row.AnomalyID, row.SourceEventID, row.SentinelEventID,
	)
	return err
}

func normalizeRules(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
}

func derivePolicyType(raw map[string]any, latestAction string) string {
	switch strings.ToLower(firstNonEmpty(
		extractString(raw, "policy_type"),
		extractString(raw, "rule_type"),
	)) {
	case "allow", "deny", "rate_limit", "threshold":
		return strings.ToLower(firstNonEmpty(extractString(raw, "policy_type"), extractString(raw, "rule_type")))
	}
	switch strings.ToLower(strings.TrimSpace(latestAction)) {
	case "drop", "block", "deny", "reject", "revoke", "remove":
		return "deny"
	case "allow", "permit", "accept":
		return "allow"
	}
	return "deny"
}

func deriveTarget(raw map[string]any) string {
	targetMap, _ := raw["target"].(map[string]any)
	if targetMap == nil {
		return "policy"
	}
	value := firstNonEmpty(
		asString(targetMap["scope_identifier"]),
		asString(targetMap["scope"]),
		asString(targetMap["producer_id"]),
		asString(targetMap["event_type"]),
		asString(targetMap["direction"]),
	)
	if value == "" {
		if ips, ok := targetMap["ips"].([]any); ok && len(ips) > 0 {
			value = asString(ips[0])
		}
	}
	if value == "" {
		value = "policy"
	}
	if len(value) > 50 {
		value = value[:50]
	}
	return value
}

func derivePriority(raw map[string]any) int {
	switch v := raw["priority"].(type) {
	case float64:
		return int(v)
	case json.Number:
		if parsed, err := strconv.Atoi(v.String()); err == nil {
			return parsed
		}
	case int:
		return v
	case int64:
		return int(v)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed
		}
	}
	return 100
}

func deriveActive(currentStatus, latestAction, latestAckResult string) bool {
	switch strings.ToLower(strings.TrimSpace(latestAction)) {
	case "revoke", "remove", "reject":
		return false
	}
	switch strings.ToLower(strings.TrimSpace(latestAckResult)) {
	case "applied", "success", "acked":
		return true
	case "failed", "rejected":
		return false
	}
	switch strings.ToLower(strings.TrimSpace(currentStatus)) {
	case "acked", "published":
		return true
	default:
		return false
	}
}

func extractString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	return asString(raw[key])
}

func nestedString(raw map[string]any, path ...string) string {
	current := any(raw)
	for _, segment := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = m[segment]
		if !ok {
			return ""
		}
	}
	return asString(current)
}

func asString(v any) string {
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func nullString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return strings.TrimSpace(v.String)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
