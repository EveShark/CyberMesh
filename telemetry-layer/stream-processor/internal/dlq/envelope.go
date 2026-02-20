package dlq

type Envelope struct {
	Schema          string `json:"schema"`
	Timestamp       int64  `json:"timestamp"`
	SourceComponent string `json:"source_component"`
	ErrorCode       string `json:"error_code"`
	ErrorMessage    string `json:"error_message"`
	PayloadHash     string `json:"payload_hash"`
}
