package model

// DeepFlowEvent represents deep inspection metadata and alerts.
type DeepFlowEvent struct {
	Schema        string            `json:"schema"`
	Timestamp     int64             `json:"ts"`
	TenantID      string            `json:"tenant_id"`
	SensorID      string            `json:"sensor_id"`
	SourceType    string            `json:"source_type"`
	SrcIP         string            `json:"src_ip"`
	DstIP         string            `json:"dst_ip"`
	SrcPort       int               `json:"src_port"`
	DstPort       int               `json:"dst_port"`
	Proto         int               `json:"proto"`
	AlertType     string            `json:"alert_type"`
	AlertCategory string            `json:"alert_category"`
	Severity      string            `json:"severity"`
	Signature     string            `json:"signature"`
	SignatureID   string            `json:"signature_id"`
	Metadata      map[string]string `json:"metadata"`
	FlowID        string            `json:"flow_id"`
}
