package api

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"
)

type policyStateReadSupport struct {
	enabled bool
}

type policyStateReadRow struct {
	PolicyID        string
	WorkflowID      sql.NullString
	CurrentStatus   sql.NullString
	LatestAckResult sql.NullString
	LatestAckReason sql.NullString
	TraceID         sql.NullString
	AnomalyID       sql.NullString
	SourceEventID   sql.NullString
	SentinelEventID sql.NullString
}

var policyStateReadCache sync.Map // map[*sql.DB]policyStateReadSupport

func loadPolicyStateReadSupport(ctx context.Context, db *sql.DB) (policyStateReadSupport, error) {
	if db == nil {
		return policyStateReadSupport{}, nil
	}
	if cached, ok := policyStateReadCache.Load(db); ok {
		return cached.(policyStateReadSupport), nil
	}
	support := policyStateReadSupport{}
	var reg sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('state_policies')::STRING`).Scan(&reg); err != nil {
		return support, err
	}
	if !reg.Valid || strings.TrimSpace(reg.String) == "" {
		policyStateReadCache.Store(db, support)
		return support, nil
	}
	const requiredColumns = 8
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'state_policies'
		  AND column_name IN (
			'policy_id_text',
			'workflow_id',
			'current_status',
			'latest_ack_result',
			'trace_id',
			'anomaly_id',
			'source_event_id',
			'sentinel_event_id'
		  )
	`).Scan(&count); err != nil {
		return support, err
	}
	support.enabled = count >= requiredColumns
	policyStateReadCache.Store(db, support)
	return support, nil
}

func loadPolicyStateRows(ctx context.Context, db *sql.DB, policyIDs []string) (map[string]policyStateReadRow, error) {
	result := make(map[string]policyStateReadRow)
	if db == nil || len(policyIDs) == 0 {
		return result, nil
	}
	support, err := loadPolicyStateReadSupport(ctx, db)
	if err != nil || !support.enabled {
		return result, err
	}
	args := make([]any, 0, len(policyIDs))
	placeholders := make([]string, 0, len(policyIDs))
	seen := make(map[string]struct{}, len(policyIDs))
	for _, policyID := range policyIDs {
		policyID = strings.TrimSpace(policyID)
		if policyID == "" {
			continue
		}
		if _, ok := seen[policyID]; ok {
			continue
		}
		seen[policyID] = struct{}{}
		args = append(args, policyID)
		placeholders = append(placeholders, "$"+strconv.Itoa(len(args)))
	}
	if len(args) == 0 {
		return result, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT
			policy_id_text,
			workflow_id,
			current_status,
			latest_ack_result,
			latest_ack_reason,
			trace_id,
			anomaly_id,
			source_event_id,
			sentinel_event_id
		FROM state_policies
		WHERE policy_id_text IN (`+strings.Join(placeholders, ", ")+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row policyStateReadRow
		if err := rows.Scan(
			&row.PolicyID,
			&row.WorkflowID,
			&row.CurrentStatus,
			&row.LatestAckResult,
			&row.LatestAckReason,
			&row.TraceID,
			&row.AnomalyID,
			&row.SourceEventID,
			&row.SentinelEventID,
		); err != nil {
			return nil, err
		}
		result[strings.TrimSpace(row.PolicyID)] = row
	}
	return result, rows.Err()
}

func overlayStateOnPolicySummary(summary *policySummaryDTO, row policyStateReadRow) {
	if summary == nil {
		return
	}
	if summary.WorkflowID == "" && row.WorkflowID.Valid {
		summary.WorkflowID = strings.TrimSpace(row.WorkflowID.String)
	}
	if row.CurrentStatus.Valid && strings.TrimSpace(row.CurrentStatus.String) != "" {
		summary.LatestStatus = strings.TrimSpace(row.CurrentStatus.String)
	}
	if row.LatestAckResult.Valid && strings.TrimSpace(row.LatestAckResult.String) != "" {
		summary.LatestAckResult = strings.TrimSpace(row.LatestAckResult.String)
	}
	if summary.TraceID == "" && row.TraceID.Valid {
		summary.TraceID = strings.TrimSpace(row.TraceID.String)
	}
	if summary.AnomalyID == "" && row.AnomalyID.Valid {
		summary.AnomalyID = strings.TrimSpace(row.AnomalyID.String)
	}
	if summary.SourceEventID == "" && row.SourceEventID.Valid {
		summary.SourceEventID = strings.TrimSpace(row.SourceEventID.String)
	}
	if summary.SentinelEventID == "" && row.SentinelEventID.Valid {
		summary.SentinelEventID = strings.TrimSpace(row.SentinelEventID.String)
	}
}

func overlayStateOnPolicySummaries(ctx context.Context, db *sql.DB, items []policySummaryDTO) error {
	if len(items) == 0 {
		return nil
	}
	policyIDs := make([]string, 0, len(items))
	for _, item := range items {
		policyIDs = append(policyIDs, item.PolicyID)
	}
	rows, err := loadPolicyStateRows(ctx, db, policyIDs)
	if err != nil {
		return err
	}
	for i := range items {
		if row, ok := rows[strings.TrimSpace(items[i].PolicyID)]; ok {
			overlayStateOnPolicySummary(&items[i], row)
		}
	}
	return nil
}

func overlayStateOnWorkflowSummaries(ctx context.Context, db *sql.DB, items []workflowSummaryDTO) error {
	if len(items) == 0 {
		return nil
	}
	policyIDs := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.LatestPolicyID) != "" {
			policyIDs = append(policyIDs, item.LatestPolicyID)
		}
	}
	rows, err := loadPolicyStateRows(ctx, db, policyIDs)
	if err != nil {
		return err
	}
	for i := range items {
		row, ok := rows[strings.TrimSpace(items[i].LatestPolicyID)]
		if !ok {
			continue
		}
		if row.CurrentStatus.Valid && strings.TrimSpace(row.CurrentStatus.String) != "" {
			items[i].LatestStatus = strings.TrimSpace(row.CurrentStatus.String)
		}
		if row.LatestAckResult.Valid && strings.TrimSpace(row.LatestAckResult.String) != "" {
			items[i].LatestAckResult = strings.TrimSpace(row.LatestAckResult.String)
		}
		if items[i].LatestTraceID == "" && row.TraceID.Valid {
			items[i].LatestTraceID = strings.TrimSpace(row.TraceID.String)
		}
	}
	return nil
}
