package policytrace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type StageClass string

const (
	StageClassRuntime      StageClass = "runtime"
	StageClassDurable      StageClass = "durable"
	StageClassAnomaly      StageClass = "anomaly"
	StageClassUnknown      StageClass = "unknown"
	StageSourceBackend                = "backend"
	StageSourceAI                     = "ai-service"
	StageSourceSentinel               = "sentinel"
	StageSourceEnforcement            = "enforcement-agent"
)

const (
	StageTelemetryIngest        = "t_telemetry_ingest"
	StageSentinelConsume        = "t_sentinel_consume"
	StageSentinelAnalysisDone   = "t_sentinel_analysis_done"
	StageSentinelEmit           = "t_sentinel_emit"
	StageAISentinelConsume      = "t_ai_sentinel_consume"
	StageAIDecisionDone         = "t_ai_decision_done"
	StageAIProducerSendStart    = "t_ai_producer_send_start"
	StageAIProducerAck          = "t_ai_producer_ack"
	StageNonceIdempotencyOK     = "t_nonce_idempotency_ok"
	StageOutboxClaimed          = "t_outbox_claimed"
	StageControlPublishStart    = "t_control_publish_start"
	StageSentinelPublishAck     = "t_sentinel_publish_ack"
	StagePolicyHandlerEnter     = "t_policy_handler_enter"
	StageDecodeDone             = "t_decode_done"
	StageBackendConsume         = "t_backend_consume"
	StageBackendVerifiedDone    = "t_backend_verified_done"
	StageMempoolEnqueued        = "t_mempool_enqueued"
	StageLeaderSelected         = "t_leader_selected"
	StageProposeStart           = "t_propose_start"
	StageProposalBroadcast      = "t_proposal_broadcast"
	StageQCFormed               = "t_qc_formed"
	StageCommit                 = "t_commit"
	StageStateApplyDone         = "t_state_apply_done"
	StageCommittedPublishStart  = "t_committed_publish_start"
	StageCommittedPublishAck    = "t_committed_publish_ack"
	StageCommittedPublishFailed = "t_committed_publish_failed"
	StageFastPublishStart       = "t_fast_publish_start"
	StageFastPublishAck         = "t_fast_publish_ack"
	StageFastPublishFailed      = "t_fast_publish_failed"
	StageOutboxReplayNoop       = "t_outbox_replay_noop"
	StageAckedWithoutPublish    = "t_acked_without_publish_marker"
	StageDuplicatePolicyCommand = "t_duplicate_policy_command"
	StageOutboxRowCreated       = "t_outbox_row_created"
	StageOutboxRowReused        = "t_outbox_row_reused"
	StageOutboxRowRefreshed     = "t_outbox_row_refreshed"
	StageControlPublishAck      = "t_control_publish_ack"
	StageOutboxMarkDone         = "t_outbox_mark_published_done"
	StageAckReceived            = "t_ack_received"
	StageAckPersisted           = "t_ack_persisted"
	StageAck                    = "t_ack"
	StageEnforcementConsume     = "t_enforcement_consume"
	StageEnforcementApplyDone   = "t_enforcement_apply_done"
	StageEnforcementPersisted   = "t_enforcement_persist_done"
	StageAckPublishStart        = "t_ack_publish_start"
	StageAckPublishDone         = "t_ack_publish_done"
	StageAckUnresolved          = "t_ack_unresolved"
	StageAckAmbiguous           = "t_ack_ambiguous"
	StageGuardrailRejected      = "t_guardrail_rejected"
)

var (
	ErrInvalidTraceEvent = errors.New("invalid control policy trace event")
)

var stageClassByName = map[string]StageClass{
	StageTelemetryIngest:        StageClassRuntime,
	StageSentinelConsume:        StageClassRuntime,
	StageSentinelAnalysisDone:   StageClassRuntime,
	StageSentinelEmit:           StageClassRuntime,
	StageAISentinelConsume:      StageClassRuntime,
	StageAIDecisionDone:         StageClassRuntime,
	StageAIProducerSendStart:    StageClassRuntime,
	StageAIProducerAck:          StageClassRuntime,
	StageNonceIdempotencyOK:     StageClassRuntime,
	StageOutboxClaimed:          StageClassRuntime,
	StageControlPublishStart:    StageClassRuntime,
	StageSentinelPublishAck:     StageClassRuntime,
	StagePolicyHandlerEnter:     StageClassRuntime,
	StageDecodeDone:             StageClassRuntime,
	StageBackendConsume:         StageClassRuntime,
	StageBackendVerifiedDone:    StageClassRuntime,
	StageMempoolEnqueued:        StageClassRuntime,
	StageLeaderSelected:         StageClassRuntime,
	StageProposeStart:           StageClassRuntime,
	StageProposalBroadcast:      StageClassRuntime,
	StageQCFormed:               StageClassRuntime,
	StageCommit:                 StageClassRuntime,
	StageStateApplyDone:         StageClassRuntime,
	StageCommittedPublishStart:  StageClassRuntime,
	StageCommittedPublishAck:    StageClassRuntime,
	StageCommittedPublishFailed: StageClassAnomaly,
	StageFastPublishStart:       StageClassRuntime,
	StageFastPublishAck:         StageClassRuntime,
	StageFastPublishFailed:      StageClassAnomaly,
	StageOutboxReplayNoop:       StageClassAnomaly,
	StageAckedWithoutPublish:    StageClassAnomaly,
	StageDuplicatePolicyCommand: StageClassAnomaly,
	StageOutboxRowCreated:       StageClassDurable,
	StageOutboxRowReused:        StageClassDurable,
	StageOutboxRowRefreshed:     StageClassDurable,
	StageControlPublishAck:      StageClassDurable,
	StageOutboxMarkDone:         StageClassDurable,
	StageAckReceived:            StageClassDurable,
	StageAckPersisted:           StageClassDurable,
	StageAck:                    StageClassDurable,
	StageEnforcementConsume:     StageClassRuntime,
	StageEnforcementApplyDone:   StageClassRuntime,
	StageEnforcementPersisted:   StageClassRuntime,
	StageAckPublishStart:        StageClassRuntime,
	StageAckPublishDone:         StageClassRuntime,
	StageAckUnresolved:          StageClassAnomaly,
	StageAckAmbiguous:           StageClassAnomaly,
	StageGuardrailRejected:      StageClassAnomaly,
}

// TraceEvent is the append-only canonical event model for control-plane lifecycle tracking.
type TraceEvent struct {
	EventID          string
	EventKey         string
	PolicyID         string
	TraceID          string
	Stage            string
	StageClass       StageClass
	StageSource      string
	TimestampMs      int64
	RequestID        string
	CommandID        string
	WorkflowID       string
	SourceEventID    string
	SentinelEventID  string
	OutboxID         string
	AckEventID       string
	RuleHash         []byte
	ScopeIdentifier  string
	Tenant           string
	Region           string
	Reason           string
	Height           uint64
	TxIndex          int
	View             uint64
	QCTsMs           int64
	Partition        int32
	Offset           int64
	AnomalyCode      string
	Details          map[string]string
	RuntimeMarkerRef string
}

func (e *TraceEvent) Normalize() error {
	if e == nil {
		return fmt.Errorf("%w: nil event", ErrInvalidTraceEvent)
	}
	e.PolicyID = strings.TrimSpace(e.PolicyID)
	e.TraceID = strings.TrimSpace(e.TraceID)
	e.Stage = strings.TrimSpace(e.Stage)
	e.StageSource = strings.TrimSpace(e.StageSource)
	e.RequestID = strings.TrimSpace(e.RequestID)
	e.CommandID = strings.TrimSpace(e.CommandID)
	e.WorkflowID = strings.TrimSpace(e.WorkflowID)
	e.SourceEventID = strings.TrimSpace(e.SourceEventID)
	e.SentinelEventID = strings.TrimSpace(e.SentinelEventID)
	e.OutboxID = strings.TrimSpace(e.OutboxID)
	e.AckEventID = strings.TrimSpace(e.AckEventID)
	e.ScopeIdentifier = strings.TrimSpace(e.ScopeIdentifier)
	e.Tenant = strings.TrimSpace(e.Tenant)
	e.Region = strings.TrimSpace(e.Region)
	e.Reason = strings.TrimSpace(e.Reason)
	e.AnomalyCode = strings.TrimSpace(e.AnomalyCode)
	e.RuntimeMarkerRef = strings.TrimSpace(e.RuntimeMarkerRef)
	if e.StageClass == "" {
		e.StageClass = ClassifyStage(e.Stage)
	}
	if e.StageClass == "" {
		e.StageClass = StageClassUnknown
	}
	if e.EventKey == "" {
		e.EventKey = MakeEventKey(*e)
	}
	if e.EventID == "" {
		if id, err := uuid.NewV7(); err == nil {
			e.EventID = id.String()
		} else {
			e.EventID = uuid.NewString()
		}
	}
	return e.Validate()
}

func (e TraceEvent) Validate() error {
	if strings.TrimSpace(e.PolicyID) == "" {
		return fmt.Errorf("%w: policy_id is required", ErrInvalidTraceEvent)
	}
	if strings.TrimSpace(e.TraceID) == "" {
		return fmt.Errorf("%w: trace_id is required", ErrInvalidTraceEvent)
	}
	if strings.TrimSpace(e.Stage) == "" {
		return fmt.Errorf("%w: stage is required", ErrInvalidTraceEvent)
	}
	if strings.TrimSpace(e.StageSource) == "" {
		return fmt.Errorf("%w: stage_source is required", ErrInvalidTraceEvent)
	}
	if e.TimestampMs <= 0 {
		return fmt.Errorf("%w: timestamp_ms must be positive", ErrInvalidTraceEvent)
	}
	if strings.TrimSpace(e.EventKey) == "" {
		return fmt.Errorf("%w: event_key is required", ErrInvalidTraceEvent)
	}
	return nil
}

func ClassifyStage(stage string) StageClass {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return StageClassUnknown
	}
	if cls, ok := stageClassByName[stage]; ok {
		return cls
	}
	return StageClassUnknown
}

func RuntimeEventFromMarker(marker Marker, source string) TraceEvent {
	if strings.TrimSpace(source) == "" {
		source = StageSourceBackend
	}
	return TraceEvent{
		PolicyID:         marker.PolicyID,
		TraceID:          marker.TraceID,
		Stage:            marker.Stage,
		StageClass:       ClassifyStage(marker.Stage),
		StageSource:      source,
		TimestampMs:      marker.TimestampMs,
		RequestID:        marker.RequestID,
		CommandID:        marker.CommandID,
		WorkflowID:       marker.WorkflowID,
		SourceEventID:    marker.SourceEventID,
		SentinelEventID:  marker.SentinelEventID,
		AckEventID:       marker.AckEventID,
		ScopeIdentifier:  marker.ScopeIdentifier,
		Tenant:           marker.Tenant,
		Region:           marker.Region,
		Reason:           marker.Reason,
		Height:           marker.Height,
		TxIndex:          marker.TxIndex,
		View:             marker.View,
		QCTsMs:           marker.QCTsMs,
		OutboxID:         marker.OutboxID,
		RuleHash:         append([]byte(nil), marker.RuleHash...),
		Partition:        marker.Partition,
		Offset:           marker.Offset,
		RuntimeMarkerRef: marker.PolicyID + ":" + marker.Stage,
	}
}

func MakeEventKey(event TraceEvent) string {
	parts := []string{
		strings.TrimSpace(event.PolicyID),
		strings.TrimSpace(event.TraceID),
		strings.TrimSpace(event.Stage),
		string(event.StageClass),
		strings.TrimSpace(event.StageSource),
		fmt.Sprintf("%d", event.TimestampMs),
		strings.TrimSpace(event.RequestID),
		strings.TrimSpace(event.CommandID),
		strings.TrimSpace(event.WorkflowID),
		strings.TrimSpace(event.SourceEventID),
		strings.TrimSpace(event.SentinelEventID),
		strings.TrimSpace(event.OutboxID),
		strings.TrimSpace(event.AckEventID),
		hex.EncodeToString(event.RuleHash),
		strings.TrimSpace(event.ScopeIdentifier),
		strings.TrimSpace(event.Tenant),
		strings.TrimSpace(event.Region),
		strings.TrimSpace(event.Reason),
		fmt.Sprintf("%d", event.Height),
		fmt.Sprintf("%d", event.TxIndex),
		fmt.Sprintf("%d", event.View),
		fmt.Sprintf("%d", event.QCTsMs),
		fmt.Sprintf("%d", event.Partition),
		fmt.Sprintf("%d", event.Offset),
		strings.TrimSpace(event.AnomalyCode),
		canonicalDetailsString(event.Details),
		strings.TrimSpace(event.RuntimeMarkerRef),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func canonicalDetailsString(details map[string]string) string {
	if len(details) == 0 {
		return ""
	}
	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(details))
	for _, key := range keys {
		ordered[key] = strings.TrimSpace(details[key])
	}
	raw, _ := json.Marshal(ordered)
	return string(raw)
}
