package model

type Identity struct {
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Node      string `json:"node,omitempty"`
}

type FlowEvent struct {
	Timestamp        int64    `json:"ts"`
	TenantID         string   `json:"tenant_id"`
	SrcIP            string   `json:"src_ip"`
	DstIP            string   `json:"dst_ip"`
	SrcPort          int      `json:"src_port"`
	DstPort          int      `json:"dst_port"`
	Proto            int      `json:"proto"`
	Bytes            int64    `json:"bytes"`
	Packets          int64    `json:"packets"`
	Direction        string   `json:"direction,omitempty"`
	Identity         Identity `json:"identity,omitempty"`
	Verdict          string   `json:"verdict,omitempty"`
	MetricsKnown     bool     `json:"metrics_known,omitempty"`
	SourceType       string   `json:"source_type,omitempty"`
	SourceID         string   `json:"source_id,omitempty"`
	TimingKnown      bool     `json:"timing_known,omitempty"`
	TimingDerived    bool     `json:"timing_derived,omitempty"`
	DerivationPolicy string   `json:"derivation_policy,omitempty"`
	FlagsKnown       bool     `json:"flags_known,omitempty"`
	FlowIatMean      float64  `json:"flow_iat_mean,omitempty"`
	FlowIatStd       float64  `json:"flow_iat_std,omitempty"`
	FlowIatMax       float64  `json:"flow_iat_max,omitempty"`
	FlowIatMin       float64  `json:"flow_iat_min,omitempty"`
	FwdIatTot        float64  `json:"fwd_iat_tot,omitempty"`
	FwdIatMean       float64  `json:"fwd_iat_mean,omitempty"`
	FwdIatStd        float64  `json:"fwd_iat_std,omitempty"`
	FwdIatMax        float64  `json:"fwd_iat_max,omitempty"`
	FwdIatMin        float64  `json:"fwd_iat_min,omitempty"`
	BwdIatTot        float64  `json:"bwd_iat_tot,omitempty"`
	BwdIatMean       float64  `json:"bwd_iat_mean,omitempty"`
	BwdIatStd        float64  `json:"bwd_iat_std,omitempty"`
	BwdIatMax        float64  `json:"bwd_iat_max,omitempty"`
	BwdIatMin        float64  `json:"bwd_iat_min,omitempty"`
	ActiveMean       float64  `json:"active_mean,omitempty"`
	ActiveStd        float64  `json:"active_std,omitempty"`
	ActiveMax        float64  `json:"active_max,omitempty"`
	ActiveMin        float64  `json:"active_min,omitempty"`
	IdleMean         float64  `json:"idle_mean,omitempty"`
	IdleStd          float64  `json:"idle_std,omitempty"`
	IdleMax          float64  `json:"idle_max,omitempty"`
	IdleMin          float64  `json:"idle_min,omitempty"`
	FinFlagCnt       float64  `json:"fin_flag_cnt,omitempty"`
	SynFlagCnt       float64  `json:"syn_flag_cnt,omitempty"`
	RstFlagCnt       float64  `json:"rst_flag_cnt,omitempty"`
	PshFlagCnt       float64  `json:"psh_flag_cnt,omitempty"`
	AckFlagCnt       float64  `json:"ack_flag_cnt,omitempty"`
	UrgFlagCnt       float64  `json:"urg_flag_cnt,omitempty"`
	CweFlagCount     float64  `json:"cwe_flag_count,omitempty"`
	EceFlagCnt       float64  `json:"ece_flag_cnt,omitempty"`
}

type FlowKey struct {
	SrcIP   string
	DstIP   string
	SrcPort int
	DstPort int
	Proto   int
}

type FlowAggregate struct {
	Schema           string   `json:"schema"`
	Timestamp        int64    `json:"ts"`
	TenantID         string   `json:"tenant_id"`
	SrcIP            string   `json:"src_ip"`
	DstIP            string   `json:"dst_ip"`
	SrcPort          int      `json:"src_port"`
	DstPort          int      `json:"dst_port"`
	Proto            int      `json:"proto"`
	FlowID           string   `json:"flow_id"`
	BytesFwd         int64    `json:"bytes_fwd"`
	BytesBwd         int64    `json:"bytes_bwd"`
	PktsFwd          int64    `json:"pkts_fwd"`
	PktsBwd          int64    `json:"pkts_bwd"`
	DurationMS       int64    `json:"duration_ms"`
	Identity         Identity `json:"identity,omitempty"`
	Verdict          string   `json:"verdict,omitempty"`
	MetricsKnown     bool     `json:"metrics_known,omitempty"`
	SourceType       string   `json:"source_type,omitempty"`
	SourceID         string   `json:"source_id,omitempty"`
	TimingKnown      bool     `json:"timing_known,omitempty"`
	TimingDerived    bool     `json:"timing_derived,omitempty"`
	DerivationPolicy string   `json:"derivation_policy,omitempty"`
	FlagsKnown       bool     `json:"flags_known,omitempty"`
	FlowIatMean      float64  `json:"flow_iat_mean,omitempty"`
	FlowIatStd       float64  `json:"flow_iat_std,omitempty"`
	FlowIatMax       float64  `json:"flow_iat_max,omitempty"`
	FlowIatMin       float64  `json:"flow_iat_min,omitempty"`
	FwdIatTot        float64  `json:"fwd_iat_tot,omitempty"`
	FwdIatMean       float64  `json:"fwd_iat_mean,omitempty"`
	FwdIatStd        float64  `json:"fwd_iat_std,omitempty"`
	FwdIatMax        float64  `json:"fwd_iat_max,omitempty"`
	FwdIatMin        float64  `json:"fwd_iat_min,omitempty"`
	BwdIatTot        float64  `json:"bwd_iat_tot,omitempty"`
	BwdIatMean       float64  `json:"bwd_iat_mean,omitempty"`
	BwdIatStd        float64  `json:"bwd_iat_std,omitempty"`
	BwdIatMax        float64  `json:"bwd_iat_max,omitempty"`
	BwdIatMin        float64  `json:"bwd_iat_min,omitempty"`
	ActiveMean       float64  `json:"active_mean,omitempty"`
	ActiveStd        float64  `json:"active_std,omitempty"`
	ActiveMax        float64  `json:"active_max,omitempty"`
	ActiveMin        float64  `json:"active_min,omitempty"`
	IdleMean         float64  `json:"idle_mean,omitempty"`
	IdleStd          float64  `json:"idle_std,omitempty"`
	IdleMax          float64  `json:"idle_max,omitempty"`
	IdleMin          float64  `json:"idle_min,omitempty"`
	FinFlagCnt       float64  `json:"fin_flag_cnt,omitempty"`
	SynFlagCnt       float64  `json:"syn_flag_cnt,omitempty"`
	RstFlagCnt       float64  `json:"rst_flag_cnt,omitempty"`
	PshFlagCnt       float64  `json:"psh_flag_cnt,omitempty"`
	AckFlagCnt       float64  `json:"ack_flag_cnt,omitempty"`
	UrgFlagCnt       float64  `json:"urg_flag_cnt,omitempty"`
	CweFlagCount     float64  `json:"cwe_flag_count,omitempty"`
	EceFlagCnt       float64  `json:"ece_flag_cnt,omitempty"`
}
