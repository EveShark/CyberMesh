package spectra

import "time"

// Envelope is the canonical event shape expected from Spectra integrations.
// It is intentionally strict to keep downstream processing deterministic.
type Envelope struct {
	AppID         string            `json:"app_id"`
	Env           string            `json:"env,omitempty"`
	SourceType    string            `json:"source_type"`
	Modality      string            `json:"modality,omitempty"`
	EventCategory string            `json:"event_category"`
	EventName     string            `json:"event_name"`
	TenantID      string            `json:"tenant_id,omitempty"`
	UserID        string            `json:"user_id,omitempty"`
	RequestID     string            `json:"request_id,omitempty"`
	TraceID       string            `json:"trace_id,omitempty"`
	SourceEventID string            `json:"source_event_id,omitempty"`
	TimestampMs   int64             `json:"ts_ms"`
	SourceID      string            `json:"source_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Features      map[string]any    `json:"features,omitempty"`
	Attributes    map[string]any    `json:"attributes,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Network       *Network          `json:"network,omitempty"`
}

type Network struct {
	SrcIP      string `json:"src_ip"`
	DstIP      string `json:"dst_ip"`
	SrcPort    int    `json:"src_port"`
	DstPort    int    `json:"dst_port"`
	Proto      int    `json:"proto"`
	BytesFwd   int64  `json:"bytes_fwd"`
	BytesBwd   int64  `json:"bytes_bwd"`
	PktsFwd    int64  `json:"pkts_fwd"`
	PktsBwd    int64  `json:"pkts_bwd"`
	DurationMS int64  `json:"duration_ms"`
}

type Config struct {
	ListenAddr               string
	WebhookPath              string
	WebhookSecret            string
	WebhookRequireSignature  bool
	SignatureTimestampHeader string
	SignatureHeader          string
	ReplayWindow             time.Duration
	DedupeTTL                time.Duration
	FlowEncoding             string
	DeepFlowEncoding         string
	FlowTopic                string
	DeepFlowTopic            string
	RequestMirrorDeepFlow    bool
	PollURL                  string
	PollInterval             time.Duration
	PollAuthBearer           string
	AppIDDefault             string
	SourceIDDefault          string
}
