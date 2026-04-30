package policytrace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type EventStore interface {
	Append(ctx context.Context, event TraceEvent) error
	AppendBatch(ctx context.Context, events []TraceEvent) error
}

type ExecContextRunner interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) RecordRuntimeMarker(marker Marker) error {
	if s == nil || s.db == nil {
		return nil
	}
	event := RuntimeEventFromMarker(marker, markerStageSource(marker.Stage))
	return s.Append(context.Background(), event)
}

func (s *Store) RecordRuntimeMarkers(ctx context.Context, markers []Marker) error {
	if s == nil || s.db == nil || len(markers) == 0 {
		return nil
	}
	events := make([]TraceEvent, 0, len(markers))
	for _, marker := range markers {
		if marker.PolicyID == "" || marker.TraceID == "" || marker.Stage == "" || marker.TimestampMs <= 0 {
			continue
		}
		events = append(events, RuntimeEventFromMarker(marker, markerStageSource(marker.Stage)))
	}
	if len(events) == 0 {
		return nil
	}
	return s.AppendBatch(ctx, events)
}

func markerStageSource(stage string) string {
	switch strings.TrimSpace(stage) {
	case StageTelemetryIngest:
		return "telemetry"
	case StageSentinelConsume, StageSentinelAnalysisDone, StageSentinelEmit, StageSentinelPublishAck:
		return StageSourceSentinel
	case StageAISentinelConsume, StageAIDecisionDone, StageAIProducerSendStart, StageAIProducerAck:
		return StageSourceAI
	case StageEnforcementConsume, StageEnforcementApplyDone, StageEnforcementPersisted, StageAckPublishStart, StageAckPublishDone:
		return StageSourceEnforcement
	default:
		return StageSourceBackend
	}
}

func (s *Store) Append(ctx context.Context, event TraceEvent) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy trace store: not initialized")
	}
	if err := event.Normalize(); err != nil {
		return err
	}
	return s.AppendBatch(ctx, []TraceEvent{event})
}

func (s *Store) AppendBatch(ctx context.Context, events []TraceEvent) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy trace store: not initialized")
	}
	return s.AppendBatchWithExec(ctx, s.db, events)
}

func (s *Store) AppendBatchWithExec(ctx context.Context, execer ExecContextRunner, events []TraceEvent) error {
	if s == nil {
		return fmt.Errorf("policy trace store: not initialized")
	}
	if execer == nil {
		return fmt.Errorf("policy trace store: execer is nil")
	}
	if len(events) == 0 {
		return nil
	}
	valueRows := make([]string, 0, len(events))
	args := make([]interface{}, 0, len(events)*25)
	for idx := range events {
		if err := events[idx].Normalize(); err != nil {
			return err
		}
		detailsJSON := []byte("{}")
		if len(events[idx].Details) > 0 {
			raw, err := json.Marshal(events[idx].Details)
			if err != nil {
				return fmt.Errorf("policy trace store: marshal details: %w", err)
			}
			detailsJSON = raw
		}
		base := len(args)
		valueRows = append(valueRows, fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10,
			base+11, base+12, base+13, base+14, base+15, base+16, base+17, base+18, base+19,
			base+20, base+21, base+22, base+23, base+24, base+25, base+26, base+27,
		))
		args = append(args,
			events[idx].EventID,
			events[idx].EventKey,
			events[idx].PolicyID,
			events[idx].TraceID,
			events[idx].Stage,
			string(events[idx].StageClass),
			events[idx].StageSource,
			events[idx].TimestampMs,
			nullIfEmpty(events[idx].RequestID),
			nullIfEmpty(events[idx].CommandID),
			nullIfEmpty(events[idx].WorkflowID),
			nullIfEmpty(events[idx].SourceEventID),
			nullIfEmpty(events[idx].SentinelEventID),
			nullIfEmpty(events[idx].OutboxID),
			nullIfEmpty(events[idx].AckEventID),
			emptyBytesToNil(events[idx].RuleHash),
			nullIfEmpty(events[idx].ScopeIdentifier),
			nullIfEmpty(events[idx].Tenant),
			nullIfEmpty(events[idx].Region),
			nullIfEmpty(events[idx].Reason),
			zeroUintToNil(events[idx].Height),
			negativeIntToNil(events[idx].TxIndex),
			zeroUintToNil(events[idx].View),
			zeroInt64ToNil(events[idx].QCTsMs),
			zeroInt32ToNil(events[idx].Partition),
			zeroInt64ToNil(events[idx].Offset),
			detailsJSON,
		)
	}
	query := `
		INSERT INTO control_policy_trace_events (
			event_id, event_key, policy_id, trace_id, stage, stage_class, stage_source, timestamp_ms,
			request_id, command_id, workflow_id, source_event_id, sentinel_event_id, outbox_id, ack_event_id, rule_hash,
			scope_identifier, tenant, region, reason, height, tx_index, view_no, qc_ts_ms, kafka_partition, kafka_offset,
			details_json
		) VALUES ` + strings.Join(valueRows, ",") + `
		ON CONFLICT (event_key) DO NOTHING
	`
	if _, err := execer.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("policy trace store: append batch: %w", err)
	}
	return nil
}

func nullIfEmpty(value string) interface{} {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func zeroUintToNil(value uint64) interface{} {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func zeroInt64ToNil(value int64) interface{} {
	if value == 0 {
		return nil
	}
	return value
}

func zeroInt32ToNil(value int32) interface{} {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func negativeIntToNil(value int) interface{} {
	if value < 0 {
		return nil
	}
	return int64(value)
}

func emptyBytesToNil(value []byte) interface{} {
	if len(value) == 0 {
		return nil
	}
	return value
}
