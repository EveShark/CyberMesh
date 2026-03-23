package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
)

const pluginVersion = "0.1.0"
const defaultPluginBaseURL = "https://api.cybermesh.qzz.io"

type commandRunner struct {
	client *apiClient
	cfg    cliConfig
	out    io.Writer
	errOut io.Writer
}

var (
	buildCommit = "dev"
	buildTime   = "unknown"
)

type interactiveMode string

const (
	interactiveAuto   interactiveMode = "auto"
	interactiveAlways interactiveMode = "always"
	interactiveNever  interactiveMode = "never"
)

type anomalyListResponse struct {
	Anomalies []struct {
		ID          string  `json:"id"`
		Type        string  `json:"type"`
		Severity    string  `json:"severity"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Source      string  `json:"source"`
		BlockHeight uint64  `json:"block_height"`
		Timestamp   int64   `json:"timestamp"`
		Confidence  float64 `json:"confidence"`
		TxHash      string  `json:"tx_hash"`
	} `json:"anomalies"`
	Count int `json:"count"`
}

type policySummary struct {
	PolicyID            string `json:"policy_id"`
	RequestID           string `json:"request_id,omitempty"`
	CommandID           string `json:"command_id,omitempty"`
	WorkflowID          string `json:"workflow_id,omitempty"`
	AnomalyID           string `json:"anomaly_id,omitempty"`
	FlowID              string `json:"flow_id,omitempty"`
	SourceID            string `json:"source_id,omitempty"`
	SourceType          string `json:"source_type,omitempty"`
	SensorID            string `json:"sensor_id,omitempty"`
	ValidatorID         string `json:"validator_id,omitempty"`
	ScopeIdentifier     string `json:"scope_identifier,omitempty"`
	TraceID             string `json:"trace_id,omitempty"`
	SourceEventID       string `json:"source_event_id,omitempty"`
	SentinelEventID     string `json:"sentinel_event_id,omitempty"`
	LatestOutboxID      string `json:"latest_outbox_id,omitempty"`
	LatestStatus        string `json:"latest_status,omitempty"`
	LatestCreatedAt     int64  `json:"latest_created_at,omitempty"`
	LatestPublishedAt   int64  `json:"latest_published_at,omitempty"`
	LatestAckedAt       int64  `json:"latest_acked_at,omitempty"`
	LatestAckEventID    string `json:"latest_ack_event_id,omitempty"`
	LatestAckResult     string `json:"latest_ack_result,omitempty"`
	LatestAckController string `json:"latest_ack_controller,omitempty"`
	OutboxCount         int64  `json:"outbox_count,omitempty"`
	AckCount            int64  `json:"ack_count,omitempty"`
}

type policyListResult struct {
	Rows []policySummary `json:"rows"`
}

type policyDetailResult struct {
	Summary policySummary `json:"summary"`
	Trace   traceResponse `json:"trace"`
}

type policyCoverageResult struct {
	PolicyID              string  `json:"policy_id"`
	OutboxCount           int64   `json:"outbox_count"`
	PendingCount          int64   `json:"pending_count"`
	PublishingCount       int64   `json:"publishing_count"`
	PublishedCount        int64   `json:"published_count"`
	RetryCount            int64   `json:"retry_count"`
	TerminalFailedCount   int64   `json:"terminal_failed_count"`
	AckedCount            int64   `json:"acked_count"`
	PublishedOrAckedCount int64   `json:"published_or_acked_count"`
	AckHistoryCount       int64   `json:"ack_history_count"`
	LatestAckEventID      string  `json:"latest_ack_event_id,omitempty"`
	LatestAckResult       string  `json:"latest_ack_result,omitempty"`
	LatestAckController   string  `json:"latest_ack_controller,omitempty"`
	LatestAckedAt         int64   `json:"latest_acked_at,omitempty"`
	PublishCoverageRatio  float64 `json:"publish_coverage_ratio"`
	AckCoverageRatio      float64 `json:"ack_coverage_ratio"`
}

type policyAcksResult struct {
	PolicyID string   `json:"policy_id"`
	Count    int      `json:"count"`
	Acks     []ackRow `json:"acks"`
}

type outboxListResponse struct {
	Rows []struct {
		ID              string `json:"id"`
		PolicyID        string `json:"policy_id"`
		RequestID       string `json:"request_id,omitempty"`
		CommandID       string `json:"command_id,omitempty"`
		WorkflowID      string `json:"workflow_id,omitempty"`
		AnomalyID       string `json:"anomaly_id,omitempty"`
		FlowID          string `json:"flow_id,omitempty"`
		SourceID        string `json:"source_id,omitempty"`
		SourceType      string `json:"source_type,omitempty"`
		SensorID        string `json:"sensor_id,omitempty"`
		ValidatorID     string `json:"validator_id,omitempty"`
		ScopeIdentifier string `json:"scope_identifier,omitempty"`
		TraceID         string `json:"trace_id,omitempty"`
		SourceEventID   string `json:"source_event_id,omitempty"`
		SentinelEventID string `json:"sentinel_event_id,omitempty"`
		Status          string `json:"status"`
		CreatedAt       int64  `json:"created_at"`
		AckedAt         int64  `json:"acked_at,omitempty"`
		AckResult       string `json:"ack_result,omitempty"`
	} `json:"rows"`
}

type ackListResponse struct {
	Rows []struct {
		PolicyID           string `json:"policy_id"`
		AckEventID         string `json:"ack_event_id,omitempty"`
		RequestID          string `json:"request_id,omitempty"`
		CommandID          string `json:"command_id,omitempty"`
		WorkflowID         string `json:"workflow_id,omitempty"`
		ControllerInstance string `json:"controller_instance,omitempty"`
		ScopeIdentifier    string `json:"scope_identifier,omitempty"`
		Result             string `json:"result,omitempty"`
		Reason             string `json:"reason,omitempty"`
		QCReference        string `json:"qc_reference,omitempty"`
		TraceID            string `json:"trace_id,omitempty"`
		SourceEventID      string `json:"source_event_id,omitempty"`
		SentinelEventID    string `json:"sentinel_event_id,omitempty"`
		ObservedAt         int64  `json:"observed_at"`
	} `json:"rows"`
}

type validatorListResponse struct {
	Validators []struct {
		ID        string `json:"id"`
		PublicKey string `json:"public_key"`
		Status    string `json:"status"`
	} `json:"validators"`
	Total int `json:"total"`
}

type controlMutationResponse struct {
	ActionID         string `json:"action_id"`
	CommandID        string `json:"command_id,omitempty"`
	WorkflowID       string `json:"workflow_id,omitempty"`
	ActionType       string `json:"action_type"`
	IdempotentReplay bool   `json:"idempotent_replay,omitempty"`
	Outbox           struct {
		ID       string `json:"id"`
		PolicyID string `json:"policy_id"`
		Status   string `json:"status"`
	} `json:"outbox"`
}

type workflowSummary struct {
	WorkflowID          string `json:"workflow_id"`
	RequestID           string `json:"request_id,omitempty"`
	CommandID           string `json:"command_id,omitempty"`
	LatestPolicyID      string `json:"latest_policy_id,omitempty"`
	LatestOutboxID      string `json:"latest_outbox_id,omitempty"`
	LatestStatus        string `json:"latest_status,omitempty"`
	LatestTraceID       string `json:"latest_trace_id,omitempty"`
	LatestCreatedAt     int64  `json:"latest_created_at,omitempty"`
	LatestPublishedAt   int64  `json:"latest_published_at,omitempty"`
	LatestAckedAt       int64  `json:"latest_acked_at,omitempty"`
	LatestAckEventID    string `json:"latest_ack_event_id,omitempty"`
	LatestAckResult     string `json:"latest_ack_result,omitempty"`
	LatestAckController string `json:"latest_ack_controller,omitempty"`
	PolicyCount         int64  `json:"policy_count,omitempty"`
	OutboxCount         int64  `json:"outbox_count,omitempty"`
	AckCount            int64  `json:"ack_count,omitempty"`
}

type workflowListResult struct {
	Rows []workflowSummary `json:"rows"`
}

type workflowDetailResult struct {
	Summary  workflowSummary `json:"summary"`
	Policies []policySummary `json:"policies"`
	Acks     []ackRow        `json:"acks,omitempty"`
	Outbox   []outboxRow     `json:"outbox,omitempty"`
}

type workflowRollbackResult struct {
	ActionID         string      `json:"action_id"`
	CommandID        string      `json:"command_id,omitempty"`
	WorkflowID       string      `json:"workflow_id"`
	ActionType       string      `json:"action_type"`
	IdempotentReplay bool        `json:"idempotent_replay"`
	AffectedPolicies int         `json:"affected_policies"`
	Outbox           []outboxRow `json:"outbox"`
}

type auditEntry struct {
	ActionID         string `json:"action_id"`
	ActionType       string `json:"action_type"`
	TargetKind       string `json:"target_kind"`
	OutboxID         string `json:"outbox_id,omitempty"`
	WorkflowID       string `json:"workflow_id,omitempty"`
	PolicyID         string `json:"policy_id,omitempty"`
	Actor            string `json:"actor"`
	ReasonCode       string `json:"reason_code"`
	ReasonText       string `json:"reason_text"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
	RequestID        string `json:"request_id"`
	BeforeStatus     string `json:"before_status,omitempty"`
	AfterStatus      string `json:"after_status,omitempty"`
	TenantScope      string `json:"tenant_scope,omitempty"`
	Classification   string `json:"classification,omitempty"`
	BeforeLeaseEpoch int64  `json:"before_lease_epoch,omitempty"`
	AfterLeaseEpoch  int64  `json:"after_lease_epoch,omitempty"`
	DecisionHash     string `json:"decision_hash,omitempty"`
	CreatedAt        int64  `json:"created_at"`
}

type auditListResult struct {
	Rows []auditEntry `json:"rows"`
}

type auditExportResult struct {
	Format     string       `json:"format"`
	Rows       []auditEntry `json:"rows"`
	ExportedAt int64        `json:"exported_at"`
}

type controlLeaseRow struct {
	LeaseKey   string `json:"lease_key"`
	HolderID   string `json:"holder_id"`
	Epoch      int64  `json:"epoch"`
	LeaseUntil int64  `json:"lease_until"`
	UpdatedAt  int64  `json:"updated_at"`
	IsActive   bool   `json:"is_active"`
	StaleByMs  int64  `json:"stale_by_ms,omitempty"`
	NowUnixMs  int64  `json:"now_unix_ms"`
}

type controlStatusResponse struct {
	Leases                    []controlLeaseRow `json:"leases"`
	ControlMutationSafeMode   bool              `json:"control_mutation_safe_mode"`
	ControlMutationKillSwitch bool              `json:"control_mutation_kill_switch"`
}

type outboxBacklogResult struct {
	Pending            int64   `json:"pending"`
	Retry              int64   `json:"retry"`
	Publishing         int64   `json:"publishing"`
	PublishedRows      int64   `json:"published_rows"`
	AckedRows          int64   `json:"acked_rows"`
	TerminalRows       int64   `json:"terminal_rows"`
	TotalRows          int64   `json:"total_rows"`
	OldestPendingAgeMs int64   `json:"oldest_pending_age_ms"`
	AckClosureRatio    float64 `json:"ack_closure_ratio"`
}

type aiHistoryDetection struct {
	ID              string  `json:"id,omitempty"`
	Type            string  `json:"type,omitempty"`
	Severity        string  `json:"severity,omitempty"`
	Title           string  `json:"title,omitempty"`
	Description     string  `json:"description,omitempty"`
	Source          string  `json:"source,omitempty"`
	Timestamp       int64   `json:"timestamp,omitempty"`
	Confidence      float64 `json:"confidence,omitempty"`
	PolicyID        string  `json:"policy_id,omitempty"`
	WorkflowID      string  `json:"workflow_id,omitempty"`
	TraceID         string  `json:"trace_id,omitempty"`
	AnomalyID       string  `json:"anomaly_id,omitempty"`
	SentinelEventID string  `json:"sentinel_event_id,omitempty"`
}

type aiHistoryResult struct {
	Detections []aiHistoryDetection `json:"detections"`
	Count      int                  `json:"count"`
	UpdatedAt  string               `json:"updated_at"`
}

type aiSuspiciousNode struct {
	ID             string  `json:"id,omitempty"`
	Status         string  `json:"status,omitempty"`
	Uptime         float64 `json:"uptime,omitempty"`
	SuspicionScore float64 `json:"suspicion_score,omitempty"`
	Reason         string  `json:"reason,omitempty"`
}

type aiSuspiciousNodesResult struct {
	Nodes     []aiSuspiciousNode `json:"nodes"`
	UpdatedAt string             `json:"updated_at"`
}

type traceResponse struct {
	PolicyID        string           `json:"policy_id"`
	TraceID         string           `json:"trace_id,omitempty"`
	SourceEventID   string           `json:"source_event_id,omitempty"`
	SentinelEventID string           `json:"sentinel_event_id,omitempty"`
	Outbox          []outboxRow      `json:"outbox"`
	Acks            []ackRow         `json:"acks"`
	Materialized    *materializedDTO `json:"materialized,omitempty"`
}

type outboxRow struct {
	ID              string `json:"id"`
	PolicyID        string `json:"policy_id"`
	RequestID       string `json:"request_id,omitempty"`
	CommandID       string `json:"command_id,omitempty"`
	WorkflowID      string `json:"workflow_id,omitempty"`
	AnomalyID       string `json:"anomaly_id,omitempty"`
	FlowID          string `json:"flow_id,omitempty"`
	SourceID        string `json:"source_id,omitempty"`
	SourceType      string `json:"source_type,omitempty"`
	SensorID        string `json:"sensor_id,omitempty"`
	ValidatorID     string `json:"validator_id,omitempty"`
	ScopeIdentifier string `json:"scope_identifier,omitempty"`
	TraceID         string `json:"trace_id,omitempty"`
	SourceEventID   string `json:"source_event_id,omitempty"`
	SentinelEventID string `json:"sentinel_event_id,omitempty"`
	Status          string `json:"status"`
	CreatedAt       int64  `json:"created_at"`
	AckedAt         int64  `json:"acked_at,omitempty"`
	AckResult       string `json:"ack_result,omitempty"`
}

type ackRow struct {
	PolicyID           string `json:"policy_id"`
	AckEventID         string `json:"ack_event_id,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
	CommandID          string `json:"command_id,omitempty"`
	WorkflowID         string `json:"workflow_id,omitempty"`
	ControllerInstance string `json:"controller_instance,omitempty"`
	ScopeIdentifier    string `json:"scope_identifier,omitempty"`
	Result             string `json:"result,omitempty"`
	Reason             string `json:"reason,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	QCReference        string `json:"qc_reference,omitempty"`
	TraceID            string `json:"trace_id,omitempty"`
	SourceEventID      string `json:"source_event_id,omitempty"`
	SentinelEventID    string `json:"sentinel_event_id,omitempty"`
	ObservedAt         int64  `json:"observed_at"`
}

type materializedDTO struct {
	TraceID         string `json:"trace_id,omitempty"`
	SourceEventID   string `json:"source_event_id,omitempty"`
	SentinelEventID string `json:"sentinel_event_id,omitempty"`
	SourceEventTsMs int64  `json:"source_event_ts_ms,omitempty"`
	AIEventTsMs     int64  `json:"ai_event_ts_ms,omitempty"`
	FirstPolicyAck  *struct {
		ControllerInstance string `json:"controller_instance,omitempty"`
		Result             string `json:"result,omitempty"`
		Reason             string `json:"reason,omitempty"`
		ErrorCode          string `json:"error_code,omitempty"`
		AppliedAtMs        int64  `json:"applied_at_ms,omitempty"`
		AckedAtMs          int64  `json:"acked_at_ms,omitempty"`
	} `json:"first_policy_ack,omitempty"`
	LatestPolicyAck *struct {
		ControllerInstance string `json:"controller_instance,omitempty"`
		Result             string `json:"result,omitempty"`
		Reason             string `json:"reason,omitempty"`
		ErrorCode          string `json:"error_code,omitempty"`
		AppliedAtMs        int64  `json:"applied_at_ms,omitempty"`
		AckedAtMs          int64  `json:"acked_at_ms,omitempty"`
	} `json:"latest_policy_ack,omitempty"`
}

type consensusOverview struct {
	Leader      string `json:"leader,omitempty"`
	LeaderID    string `json:"leader_id,omitempty"`
	Term        uint64 `json:"term"`
	Phase       string `json:"phase"`
	ActivePeers int    `json:"active_peers"`
	QuorumSize  int    `json:"quorum_size"`
	UpdatedAt   string `json:"updated_at"`
	Proposals   []struct {
		Block     uint64 `json:"block"`
		View      uint64 `json:"view"`
		Hash      string `json:"hash,omitempty"`
		Proposer  string `json:"proposer,omitempty"`
		Timestamp int64  `json:"timestamp"`
	} `json:"proposals"`
	Votes []struct {
		Type      string `json:"type"`
		Count     uint64 `json:"count"`
		Timestamp int64  `json:"timestamp"`
	} `json:"votes"`
	SuspiciousNodes []struct {
		ID             string  `json:"id"`
		Status         string  `json:"status"`
		Uptime         float64 `json:"uptime"`
		SuspicionScore float64 `json:"suspicion_score"`
		Reason         string  `json:"reason,omitempty"`
	} `json:"suspicious_nodes"`
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg, remaining, err := parseGlobalArgs(args)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return usageError()
	}
	if hasHelpArg(remaining) {
		_, err := fmt.Fprintln(stdout, helpTextForArgs(remaining))
		return err
	}
	client, err := newAPIClient(cfg)
	if err != nil {
		return enhanceOperatorError(err)
	}
	runner := &commandRunner{client: client, cfg: cfg, out: stdout, errOut: stderr}
	return enhanceOperatorError(runner.run(ctx, remaining))
}

func (r *commandRunner) run(ctx context.Context, args []string) error {
	if hasHelpArg(args) {
		_, err := fmt.Fprintln(r.out, helpTextForArgs(args))
		return err
	}
	switch args[0] {
	case "anomalies":
		return r.runAnomalies(ctx, args[1:])
	case "policies":
		return r.runPolicies(ctx, args[1:])
	case "workflows":
		return r.runWorkflows(ctx, args[1:])
	case "audit":
		return r.runAudit(ctx, args[1:])
	case "ai":
		return r.runAI(ctx, args[1:])
	case "control":
		return r.runControl(ctx, args[1:])
	case "backlog":
		return r.runBacklog(ctx, args[1:])
	case "completion":
		return r.runCompletion(args[1:])
	case "trace":
		return r.runTrace(ctx, args[1:])
	case "monitor":
		return r.runMonitor(ctx, args[1:])
	case "outbox":
		return r.runOutbox(ctx, args[1:])
	case "ack":
		return r.runAck(ctx, args[1:])
	case "validators":
		return r.runValidators(ctx, args[1:])
	case "consensus":
		return r.runConsensus(ctx, args[1:])
	case "safe-mode":
		return r.runSafeMode(ctx, args[1:])
	case "kill-switch":
		return r.runKillSwitch(ctx, args[1:])
	case "revoke":
		return r.runRevoke(ctx, args[1:])
	case "version":
		return r.runVersion(args[1:])
	case "doctor":
		return r.runDoctor(ctx, args[1:])
	case "leases":
		return r.runLeases(ctx, args[1:])
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(r.out, helpTextForArgs(args[1:]))
		return err
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
	}
}

func (r *commandRunner) shouldUseInteractiveByDefault() bool {
	if r.cfg.Output != "table" {
		return false
	}
	switch interactiveMode(strings.TrimSpace(r.cfg.InteractiveMode)) {
	case interactiveAlways:
		return true
	case interactiveNever:
		return false
	default:
		if file, ok := r.out.(*os.File); ok {
			if info, err := file.Stat(); err == nil {
				return (info.Mode() & os.ModeCharDevice) != 0
			}
		}
		return false
	}
}

func parseGlobalArgs(args []string) (cliConfig, []string, error) {
	cfg := cliConfig{
		BaseURL:      firstNonEmpty(os.Getenv("CYBERMESH_BASE_URL"), defaultPluginBaseURL),
		Token:        os.Getenv("CYBERMESH_TOKEN"),
		APIKey:       os.Getenv("CYBERMESH_API_KEY"),
		TokenFile:    os.Getenv("CYBERMESH_TOKEN_FILE"),
		APIKeyFile:   os.Getenv("CYBERMESH_API_KEY_FILE"),
		Tenant:       os.Getenv("CYBERMESH_TENANT"),
		Output:       firstNonEmpty(strings.ToLower(strings.TrimSpace(os.Getenv("CYBERMESH_OUTPUT"))), "table"),
		MTLSCertFile: os.Getenv("CYBERMESH_MTLS_CERT_FILE"),
		MTLSKeyFile:  os.Getenv("CYBERMESH_MTLS_KEY_FILE"),
		CAFile:       os.Getenv("CYBERMESH_CA_FILE"),
	}
	mode := interactiveAuto
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			rest = append(rest, args[i:]...)
			break
		}
		name, value, split := strings.Cut(arg, "=")
		takeValue := func() (string, error) {
			if split {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "--base-url":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.BaseURL = v
		case "--token":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.Token = v
		case "--api-key":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.APIKey = v
		case "--token-file":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.TokenFile = v
		case "--api-key-file":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.APIKeyFile = v
		case "--tenant":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.Tenant = v
		case "--mtls-cert":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.MTLSCertFile = v
		case "--mtls-key":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.MTLSKeyFile = v
		case "--ca-file":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.CAFile = v
		case "-o", "--output":
			v, err := takeValue()
			if err != nil {
				return cliConfig{}, nil, err
			}
			cfg.Output = strings.ToLower(strings.TrimSpace(v))
		case "--interactive":
			mode = interactiveAlways
		case "--no-interactive":
			mode = interactiveNever
		case "-h", "--help":
			rest = append(rest, args[i:]...)
			i = len(args)
		default:
			return cliConfig{}, nil, fmt.Errorf("unknown global flag %s", name)
		}
	}
	if cfg.Output == "" {
		cfg.Output = "table"
	}
	if cfg.Output != "table" && cfg.Output != "json" {
		return cliConfig{}, nil, fmt.Errorf("output must be table or json")
	}
	if err := cfg.resolveAuthSecrets(); err != nil {
		return cliConfig{}, nil, err
	}
	cfg.InteractiveMode = string(mode)
	return cfg, rest, nil
}

func (cfg *cliConfig) resolveAuthSecrets() error {
	if strings.TrimSpace(cfg.TokenFile) != "" {
		token, err := readSecretFile(cfg.TokenFile)
		if err != nil {
			return fmt.Errorf("read token file: %w", err)
		}
		cfg.Token = token
	}
	if strings.TrimSpace(cfg.APIKeyFile) != "" {
		key, err := readSecretFile(cfg.APIKeyFile)
		if err != nil {
			return fmt.Errorf("read api key file: %w", err)
		}
		cfg.APIKey = key
	}
	return nil
}

func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("secret file is empty")
	}
	return secret, nil
}

func (r *commandRunner) runAnomalies(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: kubectl cybermesh anomalies list [--limit N] [--severity LEVEL]")
	}
	fs := flag.NewFlagSet("anomalies list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 100, "")
	severity := fs.String("severity", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	var resp anomalyListResponse
	err := r.client.do(ctx, requestOptions{
		Method: http.MethodGet,
		Path:   "/anomalies",
		Query: map[string]string{
			"limit":    fmt.Sprintf("%d", *limit),
			"severity": *severity,
		},
	}, &resp)
	if err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderAnomalies(resp)
}

func (r *commandRunner) runPolicies(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: kubectl cybermesh policies list|get|coverage|revoke|approve|reject ...")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("policies list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		limit := fs.Int("limit", 100, "")
		status := fs.String("status", "", "")
		workflowID := fs.String("workflow-id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*workflowID) != "" {
			if err := requireNonEmpty("workflow_id", *workflowID); err != nil {
				return err
			}
		}
		var resp policyListResult
		if err := r.client.do(ctx, requestOptions{
			Method: http.MethodGet,
			Path:   "/policies",
			Query: map[string]string{
				"limit":       fmt.Sprintf("%d", *limit),
				"status":      *status,
				"workflow_id": *workflowID,
			},
		}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		if r.shouldUseInteractiveByDefault() {
			return r.runPoliciesInteractive(ctx, "policies list", "", map[string]string{
				"limit":       fmt.Sprintf("%d", *limit),
				"status":      *status,
				"workflow_id": *workflowID,
			})
		}
		return r.renderPolicies(resp)
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: kubectl cybermesh policies get <policy_id>")
		}
		if err := requireUUIDLike("policy_id", args[1]); err != nil {
			return err
		}
		var resp policyDetailResult
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/policies/" + apiPathSegment(args[1])}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		if r.shouldUseInteractiveByDefault() {
			return r.runPoliciesInteractive(ctx, "policies get", args[1], map[string]string{"policy_id": args[1], "limit": "25"})
		}
		return r.renderPolicyDetail(resp)
	case "coverage":
		if len(args) < 2 {
			return fmt.Errorf("usage: kubectl cybermesh policies coverage <policy_id>")
		}
		if err := requireUUIDLike("policy_id", args[1]); err != nil {
			return err
		}
		var resp policyCoverageResult
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/policies/" + apiPathSegment(args[1]) + "/coverage"}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		return r.renderPolicyCoverage(resp)
	case "acks":
		if len(args) < 2 {
			return fmt.Errorf("usage: kubectl cybermesh policies acks <policy_id>")
		}
		if err := requireUUIDLike("policy_id", args[1]); err != nil {
			return err
		}
		var resp policyAcksResult
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/policies/acks/" + apiPathSegment(args[1])}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		return r.renderPolicyAcks(resp)
	case "revoke", "approve", "reject":
		return r.runPolicyMutation(ctx, args[0], args[1:])
	default:
		return fmt.Errorf("usage: kubectl cybermesh policies list|get|coverage|acks|revoke|approve|reject ...")
	}
}

func (r *commandRunner) runPolicyMutation(ctx context.Context, action string, args []string) error {
	fs := flag.NewFlagSet("policies "+action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reasonCode := fs.String("reason-code", "", "")
	reasonText := fs.String("reason-text", "", "")
	classification := fs.String("classification", "", "")
	workflowID := fs.String("workflow-id", "", "")
	yes := fs.Bool("yes", false, "")
	if len(args) == 0 {
		return fmt.Errorf("usage: kubectl cybermesh policies %s <policy_id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]", action)
	}
	policyID := args[0]
	if err := requireUUIDLike("policy_id", policyID); err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *reasonCode == "" || *reasonText == "" {
		return fmt.Errorf("--reason-code and --reason-text are required")
	}
	if strings.TrimSpace(*workflowID) != "" {
		if err := requireNonEmpty("workflow_id", *workflowID); err != nil {
			return err
		}
	}
	if !*yes {
		return fmt.Errorf("policy %s requires --yes", action)
	}
	if err := r.confirmDestructiveAction(fmt.Sprintf("policy %s", action), policyID); err != nil {
		return err
	}
	requestID := newRequestID()
	idemKey := newRequestID()
	headers := map[string]string{
		headerRequestID:   requestID,
		headerIdempotency: idemKey,
	}
	if strings.TrimSpace(*workflowID) != "" {
		headers[headerWorkflowID] = strings.TrimSpace(*workflowID)
	}
	body := map[string]any{
		"reason_code": *reasonCode,
		"reason_text": *reasonText,
	}
	if strings.TrimSpace(*classification) != "" {
		body["classification"] = *classification
	}
	if strings.TrimSpace(*workflowID) != "" {
		body["workflow_id"] = *workflowID
	}
	var resp controlMutationResponse
	if err := r.client.do(ctx, requestOptions{
		Method:  http.MethodPost,
		Path:    "/policies/" + apiPathSegment(policyID) + ":" + action,
		Headers: headers,
		Body:    body,
	}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderMutationResult(resp)
}

func (r *commandRunner) runWorkflows(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: kubectl cybermesh workflows list|get|rollback ...")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("workflows list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		limit := fs.Int("limit", 100, "")
		status := fs.String("status", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var resp workflowListResult
		if err := r.client.do(ctx, requestOptions{
			Method: http.MethodGet,
			Path:   "/workflows",
			Query: map[string]string{
				"limit":  fmt.Sprintf("%d", *limit),
				"status": *status,
			},
		}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		if r.shouldUseInteractiveByDefault() {
			return r.runWorkflowsInteractive(ctx, "workflows list", "", map[string]string{
				"limit":  fmt.Sprintf("%d", *limit),
				"status": *status,
			})
		}
		return r.renderWorkflows(resp)
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: kubectl cybermesh workflows get <workflow_id>")
		}
		if err := requireNonEmpty("workflow_id", args[1]); err != nil {
			return err
		}
		var resp workflowDetailResult
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/workflows/" + apiPathSegment(args[1])}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		if r.shouldUseInteractiveByDefault() {
			return r.runWorkflowsInteractive(ctx, "workflows get", args[1], map[string]string{"workflow_id": args[1], "limit": "25"})
		}
		return r.renderWorkflowDetail(resp)
	case "rollback":
		if len(args) < 2 {
			return fmt.Errorf("usage: kubectl cybermesh workflows rollback <workflow_id> --reason-code <code> --reason-text <text> --yes")
		}
		workflowID := args[1]
		if err := requireNonEmpty("workflow_id", workflowID); err != nil {
			return err
		}
		fs := flag.NewFlagSet("workflows rollback", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		reasonCode := fs.String("reason-code", "", "")
		reasonText := fs.String("reason-text", "", "")
		classification := fs.String("classification", "", "")
		yes := fs.Bool("yes", false, "")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *reasonCode == "" || *reasonText == "" {
			return fmt.Errorf("--reason-code and --reason-text are required")
		}
		if !*yes {
			return fmt.Errorf("workflow rollback requires --yes")
		}
		if err := r.confirmDestructiveAction("workflow rollback", workflowID); err != nil {
			return err
		}
		requestID := newRequestID()
		idemKey := newRequestID()
		body := map[string]any{
			"workflow_id": workflowID,
			"reason_code": *reasonCode,
			"reason_text": *reasonText,
		}
		if strings.TrimSpace(*classification) != "" {
			body["classification"] = *classification
		}
		var resp workflowRollbackResult
		if err := r.client.do(ctx, requestOptions{
			Method: http.MethodPost,
			Path:   "/workflows/" + apiPathSegment(workflowID) + ":rollback",
			Headers: map[string]string{
				headerRequestID:   requestID,
				headerIdempotency: idemKey,
				headerWorkflowID:  workflowID,
			},
			Body: body,
		}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		return r.renderWorkflowRollback(resp)
	default:
		return fmt.Errorf("usage: kubectl cybermesh workflows list|get|rollback ...")
	}
}

func (r *commandRunner) runAudit(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: kubectl cybermesh audit get|export [filters]")
	}
	switch args[0] {
	case "get", "export":
		fs := flag.NewFlagSet("audit "+args[0], flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		limit := fs.Int("limit", 100, "")
		policyID := fs.String("policy-id", "", "")
		workflowID := fs.String("workflow-id", "", "")
		actionType := fs.String("action-type", "", "")
		actor := fs.String("actor", "", "")
		window := fs.String("window", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		query := map[string]string{
			"limit":       fmt.Sprintf("%d", *limit),
			"policy_id":   *policyID,
			"workflow_id": *workflowID,
			"action_type": *actionType,
			"actor":       *actor,
			"window":      *window,
		}
		if strings.TrimSpace(*policyID) != "" {
			if err := requireUUIDLike("policy_id", *policyID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(*workflowID) != "" {
			if err := requireNonEmpty("workflow_id", *workflowID); err != nil {
				return err
			}
		}
		if args[0] == "get" {
			var resp auditListResult
			if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/audit", Query: query}, &resp); err != nil {
				return err
			}
			if r.cfg.Output == "json" {
				return writeJSON(r.out, resp)
			}
			if r.shouldUseInteractiveByDefault() {
				return r.runAuditInteractive(ctx, "audit get", query)
			}
			return r.renderAudit(resp.Rows)
		}
		var resp auditExportResult
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/audit/export", Query: query}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		return r.renderAudit(resp.Rows)
	default:
		return fmt.Errorf("usage: kubectl cybermesh audit get|export [filters]")
	}
}

func (r *commandRunner) runAI(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: kubectl cybermesh ai history|suspicious-nodes")
	}
	switch args[0] {
	case "history":
		if len(args) > 1 {
			return fmt.Errorf("usage: kubectl cybermesh ai history")
		}
		if r.cfg.Output != "json" && r.shouldUseInteractiveByDefault() {
			return r.runAIInteractive(ctx, aiTabHistory)
		}
		var resp aiHistoryResult
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/ai/history"}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		return r.renderAIHistory(resp)
	case "suspicious-nodes":
		if len(args) > 1 {
			return fmt.Errorf("usage: kubectl cybermesh ai suspicious-nodes")
		}
		if r.cfg.Output != "json" && r.shouldUseInteractiveByDefault() {
			return r.runAIInteractive(ctx, aiTabNodes)
		}
		var resp aiSuspiciousNodesResult
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/ai/suspicious-nodes"}, &resp); err != nil {
			return err
		}
		if r.cfg.Output == "json" {
			return writeJSON(r.out, resp)
		}
		return r.renderAISuspiciousNodes(resp)
	default:
		return fmt.Errorf("usage: kubectl cybermesh ai history|suspicious-nodes")
	}
}

func (r *commandRunner) runKillSwitch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: kubectl cybermesh kill-switch enable|disable --reason-code <code> --reason-text <text> --yes")
	}
	enable := false
	switch args[0] {
	case "enable":
		enable = true
	case "disable":
		enable = false
	default:
		return fmt.Errorf("usage: kubectl cybermesh kill-switch enable|disable --reason-code <code> --reason-text <text> --yes")
	}
	fs := flag.NewFlagSet("kill-switch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reasonCode := fs.String("reason-code", "", "")
	reasonText := fs.String("reason-text", "", "")
	yes := fs.Bool("yes", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *reasonCode == "" || *reasonText == "" {
		return fmt.Errorf("--reason-code and --reason-text are required")
	}
	if !*yes {
		return fmt.Errorf("kill-switch mutation requires --yes")
	}
	if err := r.confirmDestructiveAction("kill-switch toggle", args[0]); err != nil {
		return err
	}
	var resp killSwitchToggleResult
	if err := r.client.do(ctx, requestOptions{
		Method: http.MethodPost,
		Path:   "/control/kill-switch:toggle",
		Headers: map[string]string{
			headerRequestID: newRequestID(),
		},
		Body: map[string]any{
			"enabled":     enable,
			"reason_code": *reasonCode,
			"reason_text": *reasonText,
		},
	}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderKillSwitchResult(resp)
}

func (r *commandRunner) runTrace(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: kubectl cybermesh trace policy <policy_id> [--interactive] | trace trace <trace_id> [--interactive] | trace anomaly <anomaly_id> [--interactive] | trace sentinel <sentinel_event_id> [--interactive] | trace source <source_event_id> [--interactive] | trace workflow <workflow_id> [--interactive]")
	}
	interactive, err := parseBoolFlag(args[2:], "--interactive")
	if err != nil {
		return err
	}
	switch args[0] {
	case "policy":
		if err := requireUUIDLike("policy_id", args[1]); err != nil {
			return err
		}
		fetch := func(fetchCtx context.Context) (traceResponse, error) {
			var resp traceResponse
			err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/control/trace/" + apiPathSegment(args[1])}, &resp)
			return resp, err
		}
		if interactive {
			return r.runTraceInteractive(ctx, fmt.Sprintf("policy %s", args[1]), fetch)
		}
		resp, err := fetch(ctx)
		if err != nil {
			return err
		}
		return r.writeTrace(resp)
	case "trace":
		if err := requireNonEmpty("trace_id", args[1]); err != nil {
			return err
		}
		fetch := func(fetchCtx context.Context) (traceResponse, error) {
			var list outboxListResponse
			if err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/control/trace", Query: map[string]string{"trace_id": args[1]}}, &list); err != nil {
				return traceResponse{}, err
			}
			if len(list.Rows) == 0 {
				return traceResponse{}, fmt.Errorf("no policy found for trace_id %q", args[1])
			}
			var resp traceResponse
			if err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/control/trace/" + apiPathSegment(list.Rows[0].PolicyID)}, &resp); err != nil {
				return traceResponse{}, err
			}
			return resp, nil
		}
		if interactive {
			return r.runTraceInteractive(ctx, fmt.Sprintf("trace %s", args[1]), fetch)
		}
		resp, err := fetch(ctx)
		if err != nil {
			return err
		}
		return r.writeTrace(resp)
	case "anomaly":
		// Anomaly and sentinel selectors first resolve matching outbox rows, so they
		// must satisfy the outbox selector contract rather than direct trace lookup rules.
		query := map[string]string{"anomaly_id": args[1]}
		if err := validateOutboxSelectors(query); err != nil {
			return err
		}
		if interactive {
			return r.runTraceLookupInteractive(ctx, query, "anomaly_id", args[1])
		}
		return r.runTraceLookup(ctx, query, "anomaly_id", args[1])
	case "sentinel":
		// Sentinel lookups also resolve via outbox rows before fetching the trace detail.
		query := map[string]string{"sentinel_event_id": args[1]}
		if err := validateOutboxSelectors(query); err != nil {
			return err
		}
		if interactive {
			return r.runTraceLookupInteractive(ctx, query, "sentinel_event_id", args[1])
		}
		return r.runTraceLookup(ctx, query, "sentinel_event_id", args[1])
	case "source":
		// Source and workflow selectors use the direct trace lookup path instead of the
		// outbox list path, so they follow the trace lookup validator rules.
		query := map[string]string{"source_event_id": args[1]}
		if err := validateTraceLookupSelectors(query); err != nil {
			return err
		}
		if interactive {
			return r.runTraceLookupInteractive(ctx, query, "source_event_id", args[1])
		}
		return r.runTraceLookup(ctx, query, "source_event_id", args[1])
	case "workflow":
		query := map[string]string{"workflow_id": args[1]}
		if err := validateTraceLookupSelectors(query); err != nil {
			return err
		}
		if interactive {
			return r.runTraceLookupInteractive(ctx, query, "workflow_id", args[1])
		}
		return r.runTraceLookup(ctx, query, "workflow_id", args[1])
	default:
		return fmt.Errorf("usage: kubectl cybermesh trace policy <policy_id> [--interactive] | trace trace <trace_id> [--interactive] | trace anomaly <anomaly_id> [--interactive] | trace sentinel <sentinel_event_id> [--interactive] | trace source <source_event_id> [--interactive] | trace workflow <workflow_id> [--interactive]")
	}
}

func (r *commandRunner) runMonitor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	policyID := fs.String("policy-id", "", "")
	workflowID := fs.String("workflow-id", "", "")
	status := fs.String("status", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := map[string]string{
		"policy_id":   *policyID,
		"workflow_id": *workflowID,
		"status":      *status,
	}
	if err := validateMonitorSelectors(query); err != nil {
		return err
	}
	fetch := func(fetchCtx context.Context) (monitorData, error) {
		return r.fetchMonitorData(fetchCtx, query)
	}
	return r.runMonitorInteractive(ctx, fetch)
}

func (r *commandRunner) runCompletion(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kubectl cybermesh completion <bash|zsh|powershell>")
	}
	script, err := shellCompletion(args[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.out, script)
	return err
}

func (r *commandRunner) runOutbox(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "get" {
		return fmt.Errorf("usage: kubectl cybermesh outbox get [selectors]")
	}
	fs := flag.NewFlagSet("outbox get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	policyID := fs.String("policy-id", "", "")
	requestID := fs.String("request-id", "", "")
	commandID := fs.String("command-id", "", "")
	workflowID := fs.String("workflow-id", "", "")
	traceID := fs.String("trace-id", "", "")
	sentinelEventID := fs.String("sentinel-event-id", "", "")
	anomalyID := fs.String("anomaly-id", "", "")
	flowID := fs.String("flow-id", "", "")
	sourceID := fs.String("source-id", "", "")
	sourceType := fs.String("source-type", "", "")
	sensorID := fs.String("sensor-id", "", "")
	validatorID := fs.String("validator-id", "", "")
	scopeIdentifier := fs.String("scope-identifier", "", "")
	status := fs.String("status", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	query := map[string]string{
		"policy_id":         *policyID,
		"request_id":        *requestID,
		"command_id":        *commandID,
		"workflow_id":       *workflowID,
		"trace_id":          *traceID,
		"sentinel_event_id": *sentinelEventID,
		"anomaly_id":        *anomalyID,
		"flow_id":           *flowID,
		"source_id":         *sourceID,
		"source_type":       *sourceType,
		"sensor_id":         *sensorID,
		"validator_id":      *validatorID,
		"scope_identifier":  *scopeIdentifier,
		"status":            *status,
	}
	if !hasAnyValue(query) {
		return fmt.Errorf("at least one selector is required")
	}
	if err := validateOutboxSelectors(query); err != nil {
		return err
	}
	return r.runOutboxList(ctx, query)
}

func (r *commandRunner) runAck(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "get" {
		return fmt.Errorf("usage: kubectl cybermesh ack get [selectors]")
	}
	fs := flag.NewFlagSet("ack get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	policyID := fs.String("policy-id", "", "")
	traceID := fs.String("trace-id", "", "")
	sourceEventID := fs.String("source-event-id", "", "")
	sentinelEventID := fs.String("sentinel-event-id", "", "")
	ackEventID := fs.String("ack-event-id", "", "")
	requestID := fs.String("request-id", "", "")
	commandID := fs.String("command-id", "", "")
	workflowID := fs.String("workflow-id", "", "")
	result := fs.String("result", "", "")
	controllerInstance := fs.String("controller-instance", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	query := map[string]string{
		"policy_id":           *policyID,
		"trace_id":            *traceID,
		"source_event_id":     *sourceEventID,
		"sentinel_event_id":   *sentinelEventID,
		"ack_event_id":        *ackEventID,
		"request_id":          *requestID,
		"command_id":          *commandID,
		"workflow_id":         *workflowID,
		"result":              *result,
		"controller_instance": *controllerInstance,
	}
	if !hasAnyValue(query) {
		return fmt.Errorf("at least one selector is required")
	}
	if err := validateAckSelectors(query); err != nil {
		return err
	}
	var resp ackListResponse
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/acks", Query: query}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderAckRows(resp)
}

func (r *commandRunner) runValidators(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("validators", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	status := fs.String("status", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var resp validatorListResponse
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/validators", Query: map[string]string{"status": *status}}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderValidators(resp)
}

func (r *commandRunner) runConsensus(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: kubectl cybermesh consensus")
	}
	var resp consensusOverview
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/consensus/overview"}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderConsensus(resp)
}

func (r *commandRunner) runBacklog(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: kubectl cybermesh backlog")
	}
	if r.cfg.Output != "json" && r.shouldUseInteractiveByDefault() {
		return r.runBacklogInteractive(ctx)
	}
	var resp outboxBacklogResult
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/outbox/backlog"}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderBacklog(resp)
}

func (r *commandRunner) runLeases(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: kubectl cybermesh leases")
	}
	if r.cfg.Output != "json" && r.shouldUseInteractiveByDefault() {
		return r.runLeasesInteractive(ctx)
	}
	var resp controlStatusResponse
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/leases"}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderControlStatus(resp)
}

func (r *commandRunner) runSafeMode(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: kubectl cybermesh safe-mode enable|disable --reason-code <code> --reason-text <text>")
	}
	enable := false
	switch args[0] {
	case "enable":
		enable = true
	case "disable":
		enable = false
	default:
		return fmt.Errorf("usage: kubectl cybermesh safe-mode enable|disable --reason-code <code> --reason-text <text>")
	}
	fs := flag.NewFlagSet("safe-mode", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reasonCode := fs.String("reason-code", "", "")
	reasonText := fs.String("reason-text", "", "")
	yes := fs.Bool("yes", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *reasonCode == "" || *reasonText == "" {
		return fmt.Errorf("--reason-code and --reason-text are required")
	}
	if !*yes {
		return fmt.Errorf("safe-mode mutation requires --yes")
	}
	if err := r.confirmDestructiveAction("safe-mode toggle", args[0]); err != nil {
		return err
	}
	var resp safeModeToggleResult
	if err := r.client.do(ctx, requestOptions{
		Method: http.MethodPost,
		Path:   "/control/safe-mode:toggle",
		Body: map[string]any{
			"enabled":     enable,
			"reason_code": *reasonCode,
			"reason_text": *reasonText,
		},
		Headers: map[string]string{headerRequestID: newRequestID()},
	}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderSafeModeResult(resp)
}

func (r *commandRunner) runRevoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("revoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	outboxID := fs.String("outbox-id", "", "")
	reasonCode := fs.String("reason-code", "", "")
	reasonText := fs.String("reason-text", "", "")
	classification := fs.String("classification", "", "")
	workflowID := fs.String("workflow-id", "", "")
	yes := fs.Bool("yes", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outboxID == "" || *reasonCode == "" || *reasonText == "" {
		return fmt.Errorf("--outbox-id, --reason-code, and --reason-text are required")
	}
	if err := requireUUIDLike("outbox_id", *outboxID); err != nil {
		return err
	}
	if strings.TrimSpace(*workflowID) != "" {
		if err := requireNonEmpty("workflow_id", *workflowID); err != nil {
			return err
		}
	}
	if !*yes {
		return fmt.Errorf("revoke mutation requires --yes")
	}
	if err := r.confirmDestructiveAction("outbox revoke", *outboxID); err != nil {
		return err
	}
	requestID := newRequestID()
	idemKey := newRequestID()
	headers := map[string]string{
		headerRequestID:   requestID,
		headerIdempotency: idemKey,
	}
	if strings.TrimSpace(*workflowID) != "" {
		headers[headerWorkflowID] = strings.TrimSpace(*workflowID)
	}
	body := map[string]any{
		"reason_code": *reasonCode,
		"reason_text": *reasonText,
	}
	if strings.TrimSpace(*classification) != "" {
		body["classification"] = *classification
	}
	if strings.TrimSpace(*workflowID) != "" {
		body["workflow_id"] = *workflowID
	}
	var resp controlMutationResponse
	err := r.client.do(ctx, requestOptions{
		Method:  http.MethodPost,
		Path:    "/control/outbox/" + apiPathSegment(*outboxID) + ":revoke",
		Headers: headers,
		Body:    body,
	}, &resp)
	if err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderMutationResult(resp)
}

func (r *commandRunner) writeAny(v any) error {
	if r.cfg.Output == "json" {
		return writeJSON(r.out, v)
	}
	return r.renderGenericMap(v)
}

func (r *commandRunner) runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	verbose := fs.Bool("verbose", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	info := map[string]any{
		"version": pluginVersion,
	}
	if *verbose {
		info["commit"] = buildCommit
		info["build_time"] = buildTime
		info["base_url"] = r.cfg.BaseURL
		info["output"] = r.cfg.Output
		info["interactive_mode"] = r.cfg.InteractiveMode
		info["tenant"] = r.cfg.Tenant
		info["auth_mode"] = r.authMode()
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, info)
	}
	if !*verbose {
		_, err := fmt.Fprintf(r.out, "kubectl-cybermesh %s\n", pluginVersion)
		return err
	}
	renderTitle(r.out, "Version")
	renderKV(r.out, map[string]string{
		"Version":          pluginVersion,
		"Commit":           buildCommit,
		"Build Time":       buildTime,
		"Base URL":         r.cfg.BaseURL,
		"Output":           r.cfg.Output,
		"Interactive Mode": r.cfg.InteractiveMode,
		"Tenant":           blankDash(r.cfg.Tenant),
		"Auth Mode":        r.authMode(),
	})
	return nil
}

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details"`
}

type doctorResult struct {
	Version string        `json:"version"`
	Checks  []doctorCheck `json:"checks"`
}

func (r *commandRunner) runDoctor(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: kubectl cybermesh doctor")
	}
	result := doctorResult{
		Version: pluginVersion,
		Checks: []doctorCheck{
			{Name: "base_url", Status: "ok", Details: r.cfg.BaseURL},
			{Name: "output", Status: "ok", Details: r.cfg.Output},
			{Name: "interactive_mode", Status: "ok", Details: blankDash(r.cfg.InteractiveMode)},
			{Name: "tenant", Status: "ok", Details: blankDash(r.cfg.Tenant)},
			{Name: "auth_mode", Status: "ok", Details: r.authMode()},
		},
	}

	var resp consensusOverview
	err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/consensus/overview"}, &resp)
	if err != nil {
		result.Checks = append(result.Checks, doctorCheck{
			Name:    "backend_reachability",
			Status:  "fail",
			Details: err.Error(),
		})
	} else {
		result.Checks = append(result.Checks, doctorCheck{
			Name:    "backend_reachability",
			Status:  "ok",
			Details: "consensus overview reachable",
		})
	}

	if r.cfg.Output == "json" {
		return writeJSON(r.out, result)
	}
	return r.renderDoctor(result)
}

func (r *commandRunner) runOutboxList(ctx context.Context, query map[string]string) error {
	var resp outboxListResponse
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/outbox", Query: query}, &resp); err != nil {
		return err
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderOutboxRows(resp)
}

func (r *commandRunner) writeTrace(resp traceResponse) error {
	if r.cfg.Output == "json" {
		return writeJSON(r.out, resp)
	}
	return r.renderTrace(resp)
}

func flattenMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	raw, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (r *commandRunner) authMode() string {
	switch {
	case strings.TrimSpace(r.cfg.TokenFile) != "":
		return "bearer-token-file"
	case strings.TrimSpace(r.cfg.APIKeyFile) != "":
		return "api-key-file"
	case strings.TrimSpace(r.cfg.Token) != "":
		return "bearer-token"
	case strings.TrimSpace(r.cfg.APIKey) != "":
		return "api-key"
	case strings.TrimSpace(r.cfg.MTLSCertFile) != "" || strings.TrimSpace(r.cfg.MTLSKeyFile) != "":
		return "mTLS"
	default:
		return "none"
	}
}

func (r *commandRunner) confirmDestructiveAction(action, target string) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CYBERMESH_SKIP_CONFIRM_PROMPT")), "1") {
		return nil
	}
	if !isTerminalFile(os.Stdin) {
		return nil
	}
	file, ok := r.errOut.(*os.File)
	if !ok || !isTerminalFile(file) {
		return nil
	}
	if _, err := fmt.Fprintf(r.errOut, "Confirm %s for %s. Type the exact target to continue: ", action, target); err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.TrimSpace(line) != target {
		return fmt.Errorf("%s cancelled: confirmation did not match target", action)
	}
	return nil
}

func isTerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func enhanceOperatorError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	hint := ""
	switch {
	case strings.Contains(msg, "CONTROL_MUTATIONS_SAFE_MODE"):
		hint = "Hint: safe mode is enabled. Use `kubectl cybermesh control` or disable safe mode explicitly before retrying."
	case strings.Contains(msg, "CONTROL_KILL_SWITCH_ENABLED"):
		hint = "Hint: kill switch is enabled. Use `kubectl cybermesh control` or disable the kill switch before retrying destructive actions."
	case strings.Contains(msg, "INVALID_STATE_TRANSITION"):
		hint = "Hint: the selected object is already in a terminal or incompatible state. Check `policies get`, `workflows get`, or `audit get` before retrying."
	case strings.Contains(msg, "TENANT_SCOPE_FORBIDDEN"), strings.Contains(lower, "tenant scope"), strings.Contains(lower, "missing tenant"), strings.Contains(lower, "requires --tenant"):
		hint = "Hint: verify the tenant context. Mutations may require `--tenant` and the target object must belong to that tenant."
	case strings.Contains(lower, "no such host"):
		hint = "Hint: backend DNS lookup failed. Verify `--base-url`, network reachability, and local DNS resolution."
	case strings.Contains(lower, "x509"):
		hint = "Hint: TLS validation failed. Verify CA, mTLS certificate, and backend hostname settings."
	case strings.Contains(lower, "context deadline exceeded"), strings.Contains(msg, "Client.Timeout"):
		hint = "Hint: the backend request timed out. Check network reachability or retry when the cluster is less loaded."
	case strings.Contains(msg, "401"), strings.Contains(msg, "403"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "forbidden"):
		hint = "Hint: verify token, API key, mTLS settings, and operator permissions."
	}
	if hint == "" {
		return err
	}
	return fmt.Errorf("%s\n%s", msg, hint)
}

func newRequestID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.NewString()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func hasAnyValue(values map[string]string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func requireUUIDLike(name, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s is required", name)
	}
	if _, err := uuid.Parse(raw); err != nil {
		return fmt.Errorf("%s must be a valid UUID", name)
	}
	return nil
}

func requireNonEmpty(name, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func validateOutboxSelectors(query map[string]string) error {
	if value := strings.TrimSpace(query["policy_id"]); value != "" {
		if err := requireUUIDLike("policy_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["request_id"]); value != "" {
		if err := requireNonEmpty("request_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["command_id"]); value != "" {
		if err := requireNonEmpty("command_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["workflow_id"]); value != "" {
		if err := requireNonEmpty("workflow_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["trace_id"]); value != "" {
		if err := requireNonEmpty("trace_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["sentinel_event_id"]); value != "" {
		if err := requireUUIDLike("sentinel_event_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["anomaly_id"]); value != "" {
		if err := requireUUIDLike("anomaly_id", value); err != nil {
			return err
		}
	}
	return nil
}

func validateAckSelectors(query map[string]string) error {
	if value := strings.TrimSpace(query["policy_id"]); value != "" {
		if err := requireUUIDLike("policy_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["trace_id"]); value != "" {
		if err := requireNonEmpty("trace_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["source_event_id"]); value != "" {
		if err := requireNonEmpty("source_event_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["sentinel_event_id"]); value != "" {
		if err := requireUUIDLike("sentinel_event_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["ack_event_id"]); value != "" {
		if err := requireUUIDLike("ack_event_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["request_id"]); value != "" {
		if err := requireNonEmpty("request_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["command_id"]); value != "" {
		if err := requireNonEmpty("command_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["workflow_id"]); value != "" {
		if err := requireNonEmpty("workflow_id", value); err != nil {
			return err
		}
	}
	return nil
}

func validateTraceLookupSelectors(query map[string]string) error {
	if value := strings.TrimSpace(query["workflow_id"]); value != "" {
		if err := requireNonEmpty("workflow_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["source_event_id"]); value != "" {
		if err := requireNonEmpty("source_event_id", value); err != nil {
			return err
		}
	}
	return validateOutboxSelectors(query)
}

func (r *commandRunner) runTraceLookup(ctx context.Context, query map[string]string, selectorName, selectorValue string) error {
	var list outboxListResponse
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/outbox", Query: query}, &list); err != nil {
		return err
	}
	if len(list.Rows) == 0 {
		return fmt.Errorf("no policy found for %s %q", selectorName, selectorValue)
	}
	policies := make([]string, 0, len(list.Rows))
	seen := make(map[string]struct{}, len(list.Rows))
	for _, row := range list.Rows {
		pid := strings.TrimSpace(row.PolicyID)
		if pid == "" {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		policies = append(policies, pid)
	}
	if len(policies) == 0 {
		return fmt.Errorf("no policy found for %s %q", selectorName, selectorValue)
	}
	if len(policies) == 1 {
		var resp traceResponse
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/trace/" + apiPathSegment(policies[0])}, &resp); err != nil {
			return err
		}
		return r.writeTrace(resp)
	}
	if r.cfg.Output == "json" {
		return writeJSON(r.out, map[string]any{
			"selector":   selectorName,
			"value":      selectorValue,
			"policy_ids": policies,
			"rows":       list.Rows,
		})
	}
	_, _ = fmt.Fprintf(r.out, "Multiple policies matched %s %q. Refine with 'trace policy <policy_id>'.\n", selectorName, selectorValue)
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "POLICY_ID\tOUTBOX_ID\tSTATUS\tTRACE_ID\tREQUEST_ID\tWORKFLOW_ID")
	for _, row := range list.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", row.PolicyID, row.ID, row.Status, row.TraceID, row.RequestID, row.WorkflowID)
	}
	return tw.Flush()
}

func (r *commandRunner) runTraceLookupInteractive(ctx context.Context, query map[string]string, selectorName, selectorValue string) error {
	fetch := func(fetchCtx context.Context) (traceResponse, error) {
		var list outboxListResponse
		if err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/control/outbox", Query: query}, &list); err != nil {
			return traceResponse{}, err
		}
		if len(list.Rows) == 0 {
			return traceResponse{}, fmt.Errorf("no policy found for %s %q", selectorName, selectorValue)
		}
		policies := make([]string, 0, len(list.Rows))
		seen := make(map[string]struct{}, len(list.Rows))
		for _, row := range list.Rows {
			pid := strings.TrimSpace(row.PolicyID)
			if pid == "" {
				continue
			}
			if _, ok := seen[pid]; ok {
				continue
			}
			seen[pid] = struct{}{}
			policies = append(policies, pid)
		}
		if len(policies) == 0 {
			return traceResponse{}, fmt.Errorf("no policy found for %s %q", selectorName, selectorValue)
		}
		if len(policies) > 1 {
			return traceResponse{}, fmt.Errorf("multiple policies matched %s %q; refine with 'trace policy <policy_id>'", selectorName, selectorValue)
		}
		var resp traceResponse
		if err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/control/trace/" + apiPathSegment(policies[0])}, &resp); err != nil {
			return traceResponse{}, err
		}
		return resp, nil
	}
	return r.runTraceInteractive(ctx, fmt.Sprintf("%s %s", selectorName, selectorValue), fetch)
}

func (r *commandRunner) runTraceInteractive(ctx context.Context, title string, fetch traceFetcher) error {
	if r.cfg.Output == "json" {
		resp, err := fetch(ctx)
		if err != nil {
			return err
		}
		return writeJSON(r.out, resp)
	}
	if tuiSnapshotEnabled() && tuiSmokeAction() == "" {
		resp, err := fetch(ctx)
		if err != nil {
			return err
		}
		model := newTraceModel(ctx, title, fetch).(*traceModel)
		model.loading = false
		model.trace = resp
		model.err = nil
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractiveTrace(newTraceModel(ctx, title, fetch), r.out)
}

func (r *commandRunner) runMonitorInteractive(ctx context.Context, fetch monitorFetcher) error {
	if r.cfg.Output == "json" {
		data, err := fetch(ctx)
		if err != nil {
			return err
		}
		return writeJSON(r.out, data)
	}
	if tuiSnapshotEnabled() && tuiSmokeAction() == "" {
		data, err := fetch(ctx)
		if err != nil {
			return err
		}
		model := newMonitorModel(ctx, fetch).(*monitorModel)
		model.loading = false
		model.data = data
		model.err = nil
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractiveMonitor(newMonitorModel(ctx, fetch), r.out)
}

func (r *commandRunner) runPoliciesInteractive(ctx context.Context, title, initialPolicy string, query map[string]string) error {
	listFetch := func(fetchCtx context.Context) (policyListResult, error) {
		var resp policyListResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/policies", Query: query}, &resp)
		return resp, err
	}
	detailFetch := func(fetchCtx context.Context, policyID string) (policyDetailResult, error) {
		var resp policyDetailResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/policies/" + apiPathSegment(policyID)}, &resp)
		return resp, err
	}
	coverageFetch := func(fetchCtx context.Context, policyID string) (policyCoverageResult, error) {
		var resp policyCoverageResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/policies/" + apiPathSegment(policyID) + "/coverage"}, &resp)
		return resp, err
	}
	workflowFetch := func(fetchCtx context.Context, workflowID string) (workflowDetailResult, error) {
		var resp workflowDetailResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/workflows/" + apiPathSegment(workflowID)}, &resp)
		return resp, err
	}
	auditFetch := func(fetchCtx context.Context, policyID string) (auditListResult, error) {
		var resp auditListResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/audit", Query: map[string]string{"policy_id": policyID, "limit": "5"}}, &resp)
		return resp, err
	}
	mutationFetch := func(fetchCtx context.Context, action, policyID, workflowID string, draft actionDraft) (controlMutationResponse, error) {
		requestID := newRequestID()
		idemKey := newRequestID()
		headers := map[string]string{
			headerRequestID:   requestID,
			headerIdempotency: idemKey,
		}
		if strings.TrimSpace(workflowID) != "" {
			headers[headerWorkflowID] = strings.TrimSpace(workflowID)
		}
		var resp controlMutationResponse
		err := r.client.do(fetchCtx, requestOptions{
			Method:  http.MethodPost,
			Path:    "/policies/" + apiPathSegment(policyID) + ":" + action,
			Headers: headers,
			Body: map[string]any{
				"reason_code":    draft.ReasonCode,
				"reason_text":    draft.ReasonText,
				"classification": draft.Classification,
				"workflow_id":    workflowID,
			},
		}, &resp)
		return resp, err
	}
	if tuiSnapshotEnabled() && tuiSmokeAction() == "" {
		model := newPoliciesModel(ctx, title, initialPolicy, listFetch, detailFetch, coverageFetch, workflowFetch, auditFetch, mutationFetch).(*policiesModel)
		list, err := listFetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.rows = list.Rows
		if initialPolicy != "" {
			for i, row := range model.rows {
				if row.PolicyID == initialPolicy {
					model.selected = i
					break
				}
			}
		}
		if len(model.rows) > 0 {
			detail, err := detailFetch(ctx, model.selectedPolicyID())
			if err != nil {
				return err
			}
			coverage, err := coverageFetch(ctx, model.selectedPolicyID())
			if err != nil {
				return err
			}
			model.detail = detail
			model.coverage = coverage
		}
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	if tuiSnapshotEnabled() && tuiSmokeAction() != "" {
		model := newPoliciesModel(ctx, title, initialPolicy, listFetch, detailFetch, coverageFetch, workflowFetch, auditFetch, mutationFetch).(*policiesModel)
		list, err := listFetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.rows = list.Rows
		if initialPolicy != "" {
			for i, row := range model.rows {
				if row.PolicyID == initialPolicy {
					model.selected = i
					break
				}
			}
		}
		if len(model.rows) > 0 {
			selectedID := model.selectedPolicyID()
			if selectedID != "" {
				detail, err := detailFetch(ctx, selectedID)
				if err != nil {
					return err
				}
				coverage, err := coverageFetch(ctx, selectedID)
				if err != nil {
					return err
				}
				model.detail = detail
				model.coverage = coverage
				switch tuiSmokeAction() {
				case "detail":
					model.detailMode = true
				case "open-linked":
					model.detailMode = true
					model.showWorkflow = true
					model.showAudit = true
					model.showTrace = true
					if workflowID := model.selectedWorkflowID(); workflowID != "" {
						work, err := workflowFetch(ctx, workflowID)
						if err != nil {
							return err
						}
						model.linkedWork = work
						model.linkedWorkID = work.Summary.WorkflowID
					}
					audit, err := auditFetch(ctx, selectedID)
					if err != nil {
						return err
					}
					model.linkedAudit = audit
				case "open-trace":
					model.detailMode = true
					model.showTrace = true
				default:
					workflowID := model.selectedWorkflowID()
					if _, err := mutationFetch(ctx, tuiSmokeAction(), selectedID, workflowID, newActionDraft(tuiSmokeAction())); err != nil {
						return err
					}
					list, err = listFetch(ctx)
					if err != nil {
						return err
					}
					model.rows = list.Rows
					if initialPolicy != "" {
						for i, row := range model.rows {
							if row.PolicyID == initialPolicy {
								model.selected = i
								break
							}
						}
					}
					detail, err = detailFetch(ctx, selectedID)
					if err != nil {
						return err
					}
					coverage, err = coverageFetch(ctx, selectedID)
					if err != nil {
						return err
					}
					model.detail = detail
					model.coverage = coverage
					model.statusMessage = strings.ToUpper(tuiSmokeAction()) + " submitted"
				}
			}
		}
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractivePolicies(newPoliciesModel(ctx, title, initialPolicy, listFetch, detailFetch, coverageFetch, workflowFetch, auditFetch, mutationFetch), r.out)
}

func (r *commandRunner) runWorkflowsInteractive(ctx context.Context, title, initialWorkflow string, query map[string]string) error {
	listFetch := func(fetchCtx context.Context) (workflowListResult, error) {
		var resp workflowListResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/workflows", Query: query}, &resp)
		return resp, err
	}
	detailFetch := func(fetchCtx context.Context, workflowID string) (workflowDetailResult, error) {
		var resp workflowDetailResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/workflows/" + apiPathSegment(workflowID)}, &resp)
		return resp, err
	}
	policyFetch := func(fetchCtx context.Context, policyID string) (policyDetailResult, error) {
		var resp policyDetailResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/policies/" + apiPathSegment(policyID)}, &resp)
		return resp, err
	}
	auditFetch := func(fetchCtx context.Context, workflowID string) (auditListResult, error) {
		var resp auditListResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/audit", Query: map[string]string{"workflow_id": workflowID, "limit": "5"}}, &resp)
		return resp, err
	}
	rollbackFetch := func(fetchCtx context.Context, workflowID string, draft actionDraft) (workflowRollbackResult, error) {
		requestID := newRequestID()
		idemKey := newRequestID()
		var resp workflowRollbackResult
		err := r.client.do(fetchCtx, requestOptions{
			Method: http.MethodPost,
			Path:   "/workflows/" + apiPathSegment(workflowID) + ":rollback",
			Headers: map[string]string{
				headerRequestID:   requestID,
				headerIdempotency: idemKey,
				headerWorkflowID:  workflowID,
			},
			Body: map[string]any{
				"workflow_id":    workflowID,
				"reason_code":    draft.ReasonCode,
				"reason_text":    draft.ReasonText,
				"classification": draft.Classification,
			},
		}, &resp)
		return resp, err
	}
	if tuiSnapshotEnabled() && tuiSmokeAction() == "" {
		model := newWorkflowsModel(ctx, title, initialWorkflow, listFetch, detailFetch, policyFetch, auditFetch, rollbackFetch).(*workflowsModel)
		list, err := listFetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.rows = list.Rows
		if initialWorkflow != "" {
			for i, row := range model.rows {
				if row.WorkflowID == initialWorkflow {
					model.selected = i
					break
				}
			}
		}
		if len(model.rows) > 0 {
			detail, err := detailFetch(ctx, model.selectedWorkflowID())
			if err != nil {
				return err
			}
			model.detail = detail
		}
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	if tuiSnapshotEnabled() && tuiSmokeAction() != "" {
		model := newWorkflowsModel(ctx, title, initialWorkflow, listFetch, detailFetch, policyFetch, auditFetch, rollbackFetch).(*workflowsModel)
		list, err := listFetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.rows = list.Rows
		if initialWorkflow != "" {
			for i, row := range model.rows {
				if row.WorkflowID == initialWorkflow {
					model.selected = i
					break
				}
			}
		}
		if len(model.rows) > 0 {
			selectedID := model.selectedWorkflowID()
			if selectedID != "" {
				detail, err := detailFetch(ctx, selectedID)
				if err != nil {
					return err
				}
				model.detail = detail
				switch tuiSmokeAction() {
				case "detail":
					model.detailMode = true
				case "open-linked", "open-trace":
					model.detailMode = true
					model.showPolicy = true
					model.showTrace = tuiSmokeAction() == "open-trace"
					model.showAudit = true
					if policyID := model.selectedPolicyID(); policyID != "" {
						policy, err := policyFetch(ctx, policyID)
						if err != nil {
							return err
						}
						model.linkedPolicy = policy
						model.linkedPolicyID = policy.Summary.PolicyID
					}
					audit, err := auditFetch(ctx, selectedID)
					if err != nil {
						return err
					}
					model.linkedAudit = audit
				case "rollback":
					if _, err := rollbackFetch(ctx, selectedID, newActionDraft("rollback")); err != nil {
						return err
					}
					list, err = listFetch(ctx)
					if err != nil {
						return err
					}
					model.rows = list.Rows
					if initialWorkflow != "" {
						for i, row := range model.rows {
							if row.WorkflowID == initialWorkflow {
								model.selected = i
								break
							}
						}
					}
					detail, err := detailFetch(ctx, selectedID)
					if err != nil {
						return err
					}
					model.detail = detail
					model.statusMessage = "ROLLBACK submitted"
				}
			}
		}
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractiveWorkflows(newWorkflowsModel(ctx, title, initialWorkflow, listFetch, detailFetch, policyFetch, auditFetch, rollbackFetch), r.out)
}

func (r *commandRunner) runAuditInteractive(ctx context.Context, title string, query map[string]string) error {
	fetch := func(fetchCtx context.Context) (auditListResult, error) {
		var resp auditListResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/audit", Query: query}, &resp)
		return resp, err
	}
	policyFetch := func(fetchCtx context.Context, policyID string) (policyDetailResult, error) {
		var resp policyDetailResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/policies/" + apiPathSegment(policyID)}, &resp)
		return resp, err
	}
	workflowFetch := func(fetchCtx context.Context, workflowID string) (workflowDetailResult, error) {
		var resp workflowDetailResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/workflows/" + apiPathSegment(workflowID)}, &resp)
		return resp, err
	}
	if tuiSnapshotEnabled() {
		model := newAuditModel(ctx, title, fetch, policyFetch, workflowFetch).(*auditModel)
		resp, err := fetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.rows = resp.Rows
		switch tuiSmokeAction() {
		case "detail":
			model.detailMode = true
		case "open-linked", "open-trace":
			model.detailMode = true
			if len(model.rows) > 0 {
				if policyID := model.selectedPolicyID(); policyID != "" {
					policy, err := policyFetch(ctx, policyID)
					if err != nil {
						return err
					}
					model.linkedPolicy = policy
					model.linkedPolicyID = policy.Summary.PolicyID
					model.showPolicy = true
					model.showTrace = tuiSmokeAction() == "open-trace"
				}
				if workflowID := model.selectedWorkflowID(); workflowID != "" {
					work, err := workflowFetch(ctx, workflowID)
					if err != nil {
						return err
					}
					model.linkedWork = work
					model.linkedWorkID = work.Summary.WorkflowID
					model.showWorkflow = true
				}
			}
		}
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractiveAudit(newAuditModel(ctx, title, fetch, policyFetch, workflowFetch), r.out)
}

func (r *commandRunner) runBacklogInteractive(ctx context.Context) error {
	fetch := func(fetchCtx context.Context) (outboxBacklogResult, error) {
		var resp outboxBacklogResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/control/outbox/backlog"}, &resp)
		return resp, err
	}
	if tuiSnapshotEnabled() {
		model := newBacklogModel(ctx, fetch).(*backlogModel)
		resp, err := fetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.data = resp
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractiveBacklog(newBacklogModel(ctx, fetch), r.out)
}

func (r *commandRunner) runAIInteractive(ctx context.Context, initialTab aiTab) error {
	historyFetch := func(fetchCtx context.Context) (aiHistoryResult, error) {
		var resp aiHistoryResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/ai/history"}, &resp)
		return resp, err
	}
	nodesFetch := func(fetchCtx context.Context) (aiSuspiciousNodesResult, error) {
		var resp aiSuspiciousNodesResult
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/ai/suspicious-nodes"}, &resp)
		return resp, err
	}
	if tuiSnapshotEnabled() {
		model := newAIModel(ctx, initialTab, historyFetch, nodesFetch).(*aiModel)
		history, err := historyFetch(ctx)
		if err != nil {
			return err
		}
		nodes, err := nodesFetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.history = history
		model.nodes = nodes
		switch tuiSmokeAction() {
		case "detail":
			model.detailMode = true
		case "switch-tab":
			model.toggleTab()
		}
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractiveAI(newAIModel(ctx, initialTab, historyFetch, nodesFetch), r.out)
}

func (r *commandRunner) runControl(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: kubectl cybermesh control")
	}
	if r.cfg.Output == "json" {
		var resp controlStatusResponse
		if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/leases"}, &resp); err != nil {
			return err
		}
		return writeJSON(r.out, resp)
	}
	if r.shouldUseInteractiveByDefault() {
		return r.runControlInteractive(ctx)
	}
	var resp controlStatusResponse
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/leases"}, &resp); err != nil {
		return err
	}
	return r.renderControlStatus(resp)
}

func (r *commandRunner) runControlInteractive(ctx context.Context) error {
	statusFetch := func(fetchCtx context.Context) (controlStatusResponse, error) {
		var resp controlStatusResponse
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/control/leases"}, &resp)
		return resp, err
	}
	toggleFetch := func(fetchCtx context.Context, kind string, enabled bool, draft actionDraft) error {
		path := "/control/safe-mode:toggle"
		if kind == "kill-switch" {
			path = "/control/kill-switch:toggle"
		}
		return r.client.do(fetchCtx, requestOptions{
			Method: http.MethodPost,
			Path:   path,
			Headers: map[string]string{
				headerRequestID: newRequestID(),
			},
			Body: map[string]any{
				"enabled":     enabled,
				"reason_code": draft.ReasonCode,
				"reason_text": draft.ReasonText,
			},
		}, nil)
	}
	if tuiSnapshotEnabled() && tuiSmokeAction() == "" {
		model := newControlModel(ctx, statusFetch, toggleFetch).(*controlModel)
		resp, err := statusFetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.data = resp
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	if tuiSnapshotEnabled() && tuiSmokeAction() != "" {
		model := newControlModel(ctx, statusFetch, toggleFetch).(*controlModel)
		resp, err := statusFetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.data = resp
		switch tuiSmokeAction() {
		case "safe-mode-enable":
			if err := toggleFetch(ctx, "safe-mode", true, newActionDraft("safe-mode")); err != nil {
				return err
			}
			model.statusMessage = "safe-mode enabled"
		case "safe-mode-disable":
			if err := toggleFetch(ctx, "safe-mode", false, newActionDraft("safe-mode")); err != nil {
				return err
			}
			model.statusMessage = "safe-mode disabled"
		case "kill-switch-enable":
			if err := toggleFetch(ctx, "kill-switch", true, newActionDraft("kill-switch")); err != nil {
				return err
			}
			model.statusMessage = "kill-switch enabled"
		case "kill-switch-disable":
			if err := toggleFetch(ctx, "kill-switch", false, newActionDraft("kill-switch")); err != nil {
				return err
			}
			model.statusMessage = "kill-switch disabled"
		}
		resp, err = statusFetch(ctx)
		if err != nil {
			return err
		}
		model.data = resp
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractiveControl(newControlModel(ctx, statusFetch, toggleFetch), r.out)
}

func (r *commandRunner) runLeasesInteractive(ctx context.Context) error {
	statusFetch := func(fetchCtx context.Context) (controlStatusResponse, error) {
		var resp controlStatusResponse
		err := r.client.do(fetchCtx, requestOptions{Method: http.MethodGet, Path: "/control/leases"}, &resp)
		return resp, err
	}
	if tuiSnapshotEnabled() {
		model := newControlReadOnlyModel(ctx, statusFetch).(*controlModel)
		resp, err := statusFetch(ctx)
		if err != nil {
			return err
		}
		model.loading = false
		model.data = resp
		_, err = fmt.Fprintln(r.out, model.View())
		return err
	}
	return launchInteractiveControl(newControlReadOnlyModel(ctx, statusFetch), r.out)
}

func (r *commandRunner) fetchMonitorData(ctx context.Context, outboxQuery map[string]string) (monitorData, error) {
	var data monitorData
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/consensus/overview"}, &data.Consensus); err != nil {
		return monitorData{}, err
	}
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/outbox", Query: outboxQuery}, &data.Outbox); err != nil {
		return monitorData{}, err
	}
	ackQuery := map[string]string{
		"policy_id":   outboxQuery["policy_id"],
		"workflow_id": outboxQuery["workflow_id"],
	}
	if err := r.client.do(ctx, requestOptions{Method: http.MethodGet, Path: "/control/acks", Query: ackQuery}, &data.Acks); err != nil {
		return monitorData{}, err
	}
	return data, nil
}

func usageError() error {
	return fmt.Errorf("%s", usageText())
}

func usageText() string {
	return strings.TrimSpace(`
kubectl cybermesh [global flags] <command>

Global flags:
  --base-url URL               (default: ` + defaultPluginBaseURL + `, env: CYBERMESH_BASE_URL)
  --token TOKEN
  --api-key KEY
  --tenant TENANT              (env: CYBERMESH_TENANT)
  --mtls-cert FILE
  --mtls-key FILE
  --ca-file FILE
  -o, --output table|json      (env: CYBERMESH_OUTPUT)

Commands:
  anomalies list [--limit N] [--severity LEVEL]
  ai history
  ai suspicious-nodes
  backlog
  policies list [--limit N] [--status STATUS] [--workflow-id <id>]
  policies get <policy_id>
  policies coverage <policy_id>
  policies acks <policy_id>
  policies revoke <policy_id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]
  policies approve <policy_id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]
  policies reject <policy_id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]
  workflows list [--limit N] [--status STATUS]
  workflows get <workflow_id>
  workflows rollback <workflow_id> --reason-code <code> --reason-text <text> --yes [--classification <value>]
  audit get [--policy-id <id>] [--workflow-id <id>] [--action-type <type>] [--actor <actor>] [--window <duration>] [--limit N]
  audit export [--policy-id <id>] [--workflow-id <id>] [--action-type <type>] [--actor <actor>] [--window <duration>] [--limit N]
  control
  completion <bash|zsh|powershell>
  trace policy <policy_id> [--interactive]
  trace trace <trace_id> [--interactive]
  trace anomaly <anomaly_id> [--interactive]
  trace sentinel <sentinel_event_id> [--interactive]
  trace source <source_event_id> [--interactive]
  trace workflow <workflow_id> [--interactive]
  monitor [--policy-id <id>] [--workflow-id <id>] [--status <status>]
  outbox get [--policy-id <id> | --anomaly-id <id> | --request-id <id> | --command-id <id> | --workflow-id <id> | --trace-id <id> | --sentinel-event-id <id> | --source-id <id> | --source-type <type>]
  ack get [--policy-id <id> | --trace-id <id> | --source-event-id <id> | --sentinel-event-id <id> | --ack-event-id <id> | --request-id <id> | --command-id <id> | --workflow-id <id>]
  leases
  validators [--status active|inactive]
  consensus
  safe-mode enable|disable --reason-code <code> --reason-text <text> --yes
  kill-switch enable|disable --reason-code <code> --reason-text <text> --yes
  revoke --outbox-id <id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]
  version [--verbose]
  doctor
`)
}

func parseBoolFlag(args []string, name string) (bool, error) {
	enabled := false
	for _, arg := range args {
		switch strings.TrimSpace(arg) {
		case "":
			continue
		case name:
			enabled = true
		default:
			return false, fmt.Errorf("unknown flag %s", arg)
		}
	}
	return enabled, nil
}

func apiPathSegment(raw string) string {
	return url.PathEscape(strings.TrimSpace(raw))
}

func validateMonitorSelectors(query map[string]string) error {
	if value := strings.TrimSpace(query["policy_id"]); value != "" {
		if err := requireUUIDLike("policy_id", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(query["workflow_id"]); value != "" {
		if err := requireNonEmpty("workflow_id", value); err != nil {
			return err
		}
	}
	return nil
}
