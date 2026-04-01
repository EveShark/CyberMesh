package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type FieldSpec struct {
	Path     string `json:"path,omitempty"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	Const    string `json:"const,omitempty"`
}

type MappingSpec struct {
	Version string               `json:"version"`
	Fields  map[string]FieldSpec `json:"fields"`
}

func LoadMapping(path string) (MappingSpec, error) {
	if strings.TrimSpace(path) == "" {
		return MappingSpec{}, errors.New("mapping path required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return MappingSpec{}, err
	}
	var spec MappingSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return MappingSpec{}, err
	}
	if strings.TrimSpace(spec.Version) == "" {
		return MappingSpec{}, errors.New("mapping version required")
	}
	if len(spec.Fields) == 0 {
		return MappingSpec{}, errors.New("mapping fields required")
	}
	for key, field := range spec.Fields {
		if strings.TrimSpace(field.Path) == "" && strings.TrimSpace(field.Const) == "" {
			return MappingSpec{}, fmt.Errorf("field %q missing path/const", key)
		}
	}
	return spec, nil
}

func RecordFromMappedJSON(line []byte, spec MappingSpec) (Record, error) {
	if len(line) == 0 {
		return Record{}, errors.New("empty input")
	}
	rec := Record{}
	seenBytes := false
	seenPkts := false
	for key, field := range spec.Fields {
		val, ok, err := extractValue(line, field)
		if err != nil {
			return Record{}, fmt.Errorf("%s: %w", key, err)
		}
		if !ok {
			continue
		}
		switch key {
		case "ts", "timestamp":
			rec.Timestamp = toInt64(val)
		case "source_event_ts_ms":
			rec.SourceEventTsMs = normalizeEventTsMs(toInt64(val))
		case "tenant_id":
			rec.TenantID = toString(val)
		case "trace_id":
			rec.TraceID = toString(val)
		case "source_event_id":
			rec.SourceEventID = toString(val)
		case "src_ip":
			rec.SrcIP = toString(val)
		case "dst_ip":
			rec.DstIP = toString(val)
		case "src_port":
			rec.SrcPort = toInt(val)
		case "dst_port":
			rec.DstPort = toInt(val)
		case "proto":
			rec.Proto = toProto(val)
		case "bytes_fwd":
			rec.BytesFwd = toInt64(val)
			seenBytes = true
		case "bytes_bwd":
			rec.BytesBwd = toInt64(val)
			seenBytes = true
		case "pkts_fwd":
			rec.PktsFwd = toInt64(val)
			seenPkts = true
		case "pkts_bwd":
			rec.PktsBwd = toInt64(val)
			seenPkts = true
		case "duration_ms":
			rec.DurationMS = toInt64(val)
		case "verdict":
			rec.Verdict = toString(val)
		case "metrics_known":
			rec.MetricsKnown = toBool(val)
		case "source_type":
			rec.SourceType = toString(val)
		case "source_id":
			rec.SourceID = toString(val)
		case "timing_known":
			rec.TimingKnown = toBool(val)
		case "timing_derived":
			rec.TimingDerived = toBool(val)
		case "derivation_policy":
			rec.DerivationPolicy = toString(val)
		case "flags_known":
			rec.FlagsKnown = toBool(val)
		case "flow_iat_mean":
			rec.FlowIatMean = toFloat(val)
		case "flow_iat_std":
			rec.FlowIatStd = toFloat(val)
		case "flow_iat_max":
			rec.FlowIatMax = toFloat(val)
		case "flow_iat_min":
			rec.FlowIatMin = toFloat(val)
		case "fwd_iat_tot":
			rec.FwdIatTot = toFloat(val)
		case "fwd_iat_mean":
			rec.FwdIatMean = toFloat(val)
		case "fwd_iat_std":
			rec.FwdIatStd = toFloat(val)
		case "fwd_iat_max":
			rec.FwdIatMax = toFloat(val)
		case "fwd_iat_min":
			rec.FwdIatMin = toFloat(val)
		case "bwd_iat_tot":
			rec.BwdIatTot = toFloat(val)
		case "bwd_iat_mean":
			rec.BwdIatMean = toFloat(val)
		case "bwd_iat_std":
			rec.BwdIatStd = toFloat(val)
		case "bwd_iat_max":
			rec.BwdIatMax = toFloat(val)
		case "bwd_iat_min":
			rec.BwdIatMin = toFloat(val)
		case "active_mean":
			rec.ActiveMean = toFloat(val)
		case "active_std":
			rec.ActiveStd = toFloat(val)
		case "active_max":
			rec.ActiveMax = toFloat(val)
		case "active_min":
			rec.ActiveMin = toFloat(val)
		case "idle_mean":
			rec.IdleMean = toFloat(val)
		case "idle_std":
			rec.IdleStd = toFloat(val)
		case "idle_max":
			rec.IdleMax = toFloat(val)
		case "idle_min":
			rec.IdleMin = toFloat(val)
		case "fin_flag_cnt":
			rec.FinFlagCnt = toFloat(val)
		case "syn_flag_cnt":
			rec.SynFlagCnt = toFloat(val)
		case "rst_flag_cnt":
			rec.RstFlagCnt = toFloat(val)
		case "psh_flag_cnt":
			rec.PshFlagCnt = toFloat(val)
		case "ack_flag_cnt":
			rec.AckFlagCnt = toFloat(val)
		case "urg_flag_cnt":
			rec.UrgFlagCnt = toFloat(val)
		case "cwe_flag_count":
			rec.CweFlagCount = toFloat(val)
		case "ece_flag_cnt":
			rec.EceFlagCnt = toFloat(val)
		case "identity.namespace":
			rec.Identity.Namespace = toString(val)
		case "identity.pod":
			rec.Identity.Pod = toString(val)
		case "identity.node":
			rec.Identity.Node = toString(val)
		default:
			return Record{}, fmt.Errorf("unsupported field %q", key)
		}
	}
	if !rec.MetricsKnown && (seenBytes || seenPkts) {
		rec.MetricsKnown = true
	}
	return rec, nil
}

func extractValue(line []byte, field FieldSpec) (any, bool, error) {
	if strings.TrimSpace(field.Const) != "" {
		v, err := parseConst(field.Const, field.Type)
		return v, true, err
	}
	res := gjson.GetBytes(line, field.Path)
	if !res.Exists() {
		if field.Required {
			return nil, false, fmt.Errorf("missing required path %q", field.Path)
		}
		return nil, false, nil
	}
	v, err := convertResult(res, field.Type)
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

func convertResult(res gjson.Result, typ string) (any, error) {
	t := strings.TrimSpace(strings.ToLower(typ))
	switch t {
	case "", "string":
		return res.String(), nil
	case "int":
		return toInt(res.Value()), nil
	case "int64":
		return toInt64(res.Value()), nil
	case "float", "float64":
		return toFloat(res.Value()), nil
	case "bool":
		return toBool(res.Value()), nil
	case "timestamp":
		return parseTimestamp(res.Value())
	case "proto":
		return toProto(res.Value()), nil
	default:
		return nil, fmt.Errorf("unsupported type %q", typ)
	}
}

func parseConst(raw string, typ string) (any, error) {
	value := strings.TrimSpace(raw)
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "", "string":
		return value, nil
	case "int":
		v, err := strconv.Atoi(value)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "int64":
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "float", "float64":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "bool":
		v, err := parseBool(value)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "timestamp":
		return parseTimestamp(value)
	case "proto":
		return toProto(value), nil
	default:
		return nil, fmt.Errorf("unsupported const type %q", typ)
	}
}

func parseTimestamp(value any) (int64, error) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return 0, errors.New("empty timestamp")
		}
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			t, err = time.Parse(time.RFC3339, v)
			if err != nil {
				return 0, err
			}
		}
		return t.Unix(), nil
	default:
		return toInt64(value), nil
	}
}

func toString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(v))
		return i
	default:
		return 0
	}
}

func toInt64(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return i
	default:
		return 0
	}
}

func normalizeEventTsMs(v int64) int64 {
	if v <= 0 {
		return 0
	}
	// ns -> ms
	if v >= 1_000_000_000_000_000_000 {
		return v / 1_000_000
	}
	// us -> ms
	if v >= 1_000_000_000_000_000 {
		return v / 1_000
	}
	// ms
	if v >= 1_000_000_000_000 {
		return v
	}
	// s -> ms
	return v * 1000
}

func toFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		return 0
	}
}

func toBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		b, _ := parseBool(v)
		return b
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true, nil
	case "false", "0", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", value)
	}
}

func toProto(value any) int {
	switch v := value.(type) {
	case string:
		switch strings.ToUpper(strings.TrimSpace(v)) {
		case "TCP":
			return 6
		case "UDP":
			return 17
		case "ICMP":
			return 1
		}
		return toInt(v)
	default:
		return toInt(v)
	}
}
