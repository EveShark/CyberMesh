package lifecycleaudit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgconn"
	"github.com/lib/pq"
)

const (
	SystemActor           = "system:lifecycle"
	ClassificationAuto    = "automatic"
	ActionPolicyCreated   = "policy_created"
	ActionPolicyPublished = "policy_published"
	ActionPolicyAcked     = "policy_acked"
	ActionPolicyFailed    = "policy_terminal_failed"
)

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type OutboxEvent struct {
	ActionType   string
	OutboxID     string
	PolicyID     string
	WorkflowID   string
	RequestID    string
	ReasonCode   string
	ReasonText   string
	BeforeStatus string
	AfterStatus  string
	TenantScope  string
}

func InsertOutboxEvent(ctx context.Context, exec queryRower, evt OutboxEvent) (string, error) {
	if exec == nil {
		return "", fmt.Errorf("lifecycle audit: executor required")
	}
	if strings.TrimSpace(evt.ActionType) == "" {
		return "", fmt.Errorf("lifecycle audit: action type required")
	}
	if strings.TrimSpace(evt.OutboxID) == "" {
		return "", fmt.Errorf("lifecycle audit: outbox id required")
	}
	if strings.TrimSpace(evt.PolicyID) == "" {
		return "", fmt.Errorf("lifecycle audit: policy id required")
	}
	if strings.TrimSpace(evt.ReasonCode) == "" {
		return "", fmt.Errorf("lifecycle audit: reason code required")
	}
	if strings.TrimSpace(evt.ReasonText) == "" {
		return "", fmt.Errorf("lifecycle audit: reason text required")
	}

	idempotencyKey := buildIdempotencyKey(evt.ActionType, evt.OutboxID)
	requestID := strings.TrimSpace(evt.RequestID)
	if requestID == "" {
		requestID = idempotencyKey
	}

	var workflowArg any
	if v := strings.TrimSpace(evt.WorkflowID); v != "" {
		workflowArg = v
	}
	var tenantArg any
	if v := strings.TrimSpace(evt.TenantScope); v != "" {
		tenantArg = v
	}
	var beforeArg any
	if v := strings.TrimSpace(evt.BeforeStatus); v != "" {
		beforeArg = v
	}
	var afterArg any
	if v := strings.TrimSpace(evt.AfterStatus); v != "" {
		afterArg = v
	}

	decisionRaw := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		evt.ActionType,
		evt.OutboxID,
		evt.PolicyID,
		requestID,
		evt.ReasonCode,
		evt.BeforeStatus,
		evt.AfterStatus,
	)
	sum := sha256.Sum256([]byte(decisionRaw))
	decisionHash := hex.EncodeToString(sum[:])

	var actionID string
	err := exec.QueryRowContext(ctx, `
		INSERT INTO control_actions_journal (
			action_type, target_kind, outbox_id, lease_key, workflow_id, policy_id, actor,
			reason_code, reason_text, idempotency_key, request_id,
			before_status, after_status, tenant_scope, classification,
			before_lease_epoch, after_lease_epoch, decision_hash
		)
		VALUES ($1, 'outbox', $2::UUID, NULL, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULL, NULL, $14)
		ON CONFLICT (action_type, idempotency_key, actor) DO UPDATE
		SET request_id = control_actions_journal.request_id
		RETURNING action_id::STRING
	`,
		evt.ActionType,
		evt.OutboxID,
		workflowArg,
		evt.PolicyID,
		SystemActor,
		evt.ReasonCode,
		evt.ReasonText,
		idempotencyKey,
		requestID,
		beforeArg,
		afterArg,
		tenantArg,
		ClassificationAuto,
		decisionHash,
	).Scan(&actionID)
	if err != nil {
		return "", err
	}
	return actionID, nil
}

func InsertOutboxEvents(ctx context.Context, exec execer, events []OutboxEvent) error {
	if exec == nil {
		return fmt.Errorf("lifecycle audit: executor required")
	}
	if len(events) == 0 {
		return nil
	}

	args := make([]any, 0, len(events)*14)
	var q strings.Builder
	q.WriteString(`
		INSERT INTO control_actions_journal (
			action_type, target_kind, outbox_id, lease_key, workflow_id, policy_id, actor,
			reason_code, reason_text, idempotency_key, request_id,
			before_status, after_status, tenant_scope, classification,
			before_lease_epoch, after_lease_epoch, decision_hash
		) VALUES
	`)

	for i, evt := range events {
		if _, err := normalizeEvent(evt); err != nil {
			return err
		}
		norm, _ := normalizeEvent(evt)
		if i > 0 {
			q.WriteString(",")
		}
		base := i * 14
		fmt.Fprintf(&q, "($%d,'outbox',$%d::UUID,NULL,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,NULL,NULL,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11, base+12, base+13, base+14)
		args = append(args,
			norm.ActionType,
			norm.OutboxID,
			norm.workflowArg,
			norm.PolicyID,
			SystemActor,
			norm.ReasonCode,
			norm.ReasonText,
			norm.idempotencyKey,
			norm.requestID,
			norm.beforeArg,
			norm.afterArg,
			norm.tenantArg,
			ClassificationAuto,
			norm.decisionHash,
		)
	}
	q.WriteString(`
		ON CONFLICT (action_type, idempotency_key, actor) DO UPDATE
		SET request_id = control_actions_journal.request_id
	`)
	_, err := exec.ExecContext(ctx, q.String(), args...)
	return err
}

func IsSchemaMismatchErr(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "42703":
			return journalSchemaVariantMatches(pgErr.Message, pgErr.ColumnName, pgErr.TableName)
		case "42P01":
			return journalRelationMissingMatches(pgErr.Message, pgErr.TableName)
		default:
			return false
		}
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch string(pqErr.Code) {
		case "42703":
			return journalSchemaVariantMatches(pqErr.Message, pqErr.Column, pqErr.Table)
		case "42P01":
			return journalRelationMissingMatches(pqErr.Message, pqErr.Table)
		default:
			return false
		}
	}
	msg := strings.ToLower(err.Error())
	if journalRelationMissingMatches(msg) {
		return true
	}
	if !strings.Contains(msg, "control_actions_journal") || !strings.Contains(msg, "undefined column") {
		return false
	}
	for _, col := range []string{"workflow_id", "policy_id", "before_lease_epoch", "after_lease_epoch", "decision_hash"} {
		if strings.Contains(msg, col) {
			return true
		}
	}
	return false
}

// IsBestEffortErr reports whether lifecycle audit write errors are safe to
// downgrade on durable control-plane hot paths.
func IsBestEffortErr(err error) bool {
	if err == nil {
		return false
	}
	if IsSchemaMismatchErr(err) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "57014", "40001", "40P01", "55P03":
			return true
		}
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch string(pqErr.Code) {
		case "57014", "40001", "40P01", "55P03":
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "sqlstate 40001") ||
		strings.Contains(msg, "restart transaction") ||
		strings.Contains(msg, "retry_serializable")
}

func journalSchemaVariantMatches(parts ...string) bool {
	joined := strings.ToLower(strings.Join(parts, " "))
	if !strings.Contains(joined, "control_actions_journal") {
		return false
	}
	for _, col := range []string{"workflow_id", "policy_id", "before_lease_epoch", "after_lease_epoch", "decision_hash"} {
		if strings.Contains(joined, col) {
			return true
		}
	}
	return false
}

func journalRelationMissingMatches(parts ...string) bool {
	joined := strings.ToLower(strings.Join(parts, " "))
	if !strings.Contains(joined, "control_actions_journal") {
		return false
	}
	return strings.Contains(joined, "relation does not exist") ||
		strings.Contains(joined, "undefined table") ||
		strings.Contains(joined, "does not exist")
}

type normalizedEvent struct {
	OutboxEvent
	idempotencyKey string
	requestID      string
	decisionHash   string
	workflowArg    any
	tenantArg      any
	beforeArg      any
	afterArg       any
}

func normalizeEvent(evt OutboxEvent) (normalizedEvent, error) {
	if strings.TrimSpace(evt.ActionType) == "" {
		return normalizedEvent{}, fmt.Errorf("lifecycle audit: action type required")
	}
	if strings.TrimSpace(evt.OutboxID) == "" {
		return normalizedEvent{}, fmt.Errorf("lifecycle audit: outbox id required")
	}
	if strings.TrimSpace(evt.PolicyID) == "" {
		return normalizedEvent{}, fmt.Errorf("lifecycle audit: policy id required")
	}
	if strings.TrimSpace(evt.ReasonCode) == "" {
		return normalizedEvent{}, fmt.Errorf("lifecycle audit: reason code required")
	}
	if strings.TrimSpace(evt.ReasonText) == "" {
		return normalizedEvent{}, fmt.Errorf("lifecycle audit: reason text required")
	}

	idempotencyKey := buildIdempotencyKey(evt.ActionType, evt.OutboxID)
	requestID := strings.TrimSpace(evt.RequestID)
	if requestID == "" {
		requestID = idempotencyKey
	}

	var workflowArg any
	if v := strings.TrimSpace(evt.WorkflowID); v != "" {
		workflowArg = v
	}
	var tenantArg any
	if v := strings.TrimSpace(evt.TenantScope); v != "" {
		tenantArg = v
	}
	var beforeArg any
	if v := strings.TrimSpace(evt.BeforeStatus); v != "" {
		beforeArg = v
	}
	var afterArg any
	if v := strings.TrimSpace(evt.AfterStatus); v != "" {
		afterArg = v
	}

	decisionRaw := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		evt.ActionType,
		evt.OutboxID,
		evt.PolicyID,
		requestID,
		evt.ReasonCode,
		evt.BeforeStatus,
		evt.AfterStatus,
	)
	sum := sha256.Sum256([]byte(decisionRaw))

	return normalizedEvent{
		OutboxEvent:    evt,
		idempotencyKey: idempotencyKey,
		requestID:      requestID,
		decisionHash:   hex.EncodeToString(sum[:]),
		workflowArg:    workflowArg,
		tenantArg:      tenantArg,
		beforeArg:      beforeArg,
		afterArg:       afterArg,
	}, nil
}

func buildIdempotencyKey(actionType, outboxID string) string {
	return "lifecycle:" + strings.TrimSpace(actionType) + ":" + strings.TrimSpace(outboxID)
}
