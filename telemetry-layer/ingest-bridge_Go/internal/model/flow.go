package model

type Identity struct {
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Node      string `json:"node,omitempty"`
}

type FlowEvent struct {
	Timestamp    int64    `json:"ts"`
	TenantID     string   `json:"tenant_id"`
	SrcIP        string   `json:"src_ip"`
	DstIP        string   `json:"dst_ip"`
	SrcPort      int      `json:"src_port"`
	DstPort      int      `json:"dst_port"`
	Proto        int      `json:"proto"`
	Bytes        int64    `json:"bytes"`
	Packets      int64    `json:"packets"`
	Direction    string   `json:"direction,omitempty"`
	Identity     Identity `json:"identity,omitempty"`
	Verdict      string   `json:"verdict,omitempty"`
	MetricsKnown bool     `json:"metrics_known,omitempty"`
	SourceType   string   `json:"source_type,omitempty"`
	SourceID     string   `json:"source_id,omitempty"`
}

type FlowKey struct {
	SrcIP   string
	DstIP   string
	SrcPort int
	DstPort int
	Proto   int
}

type FlowAggregate struct {
	Schema       string   `json:"schema"`
	Timestamp    int64    `json:"ts"`
	TenantID     string   `json:"tenant_id"`
	SrcIP        string   `json:"src_ip"`
	DstIP        string   `json:"dst_ip"`
	SrcPort      int      `json:"src_port"`
	DstPort      int      `json:"dst_port"`
	Proto        int      `json:"proto"`
	FlowID       string   `json:"flow_id"`
	BytesFwd     int64    `json:"bytes_fwd"`
	BytesBwd     int64    `json:"bytes_bwd"`
	PktsFwd      int64    `json:"pkts_fwd"`
	PktsBwd      int64    `json:"pkts_bwd"`
	DurationMS   int64    `json:"duration_ms"`
	Identity     Identity `json:"identity,omitempty"`
	Verdict      string   `json:"verdict,omitempty"`
	MetricsKnown bool     `json:"metrics_known,omitempty"`
	SourceType   string   `json:"source_type,omitempty"`
	SourceID     string   `json:"source_id,omitempty"`
}
