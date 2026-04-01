package parser

import (
	"errors"
	"strings"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/model"
	"github.com/tidwall/gjson"
)

func ParseGCPVPC(line []byte) ([]model.Record, error) {
	if len(line) == 0 {
		return nil, errors.New("empty input")
	}
	root := gjson.ParseBytes(line)
	payload := root.Get("jsonPayload")
	if !payload.Exists() {
		return nil, errors.New("missing jsonPayload")
	}
	rec := model.Record{}
	rec.SourceType = "gcp_vpc"
	rec.Timestamp = parseTimestamp(payload.Get("start_time").Value())
	if rec.Timestamp == 0 {
		rec.Timestamp = parseTimestamp(root.Get("timestamp").Value())
	}
	if rec.Timestamp == 0 {
		rec.Timestamp = parseTimestamp(root.Get("receiveTimestamp").Value())
	}
	src := payload.Get("connection.src_ip").String()
	dst := payload.Get("connection.dest_ip").String()
	if src == "" {
		src = payload.Get("src_ip").String()
	}
	if dst == "" {
		dst = payload.Get("dest_ip").String()
	}
	rec.SrcIP = src
	rec.DstIP = dst
	if payload.Get("connection.src_port").Exists() {
		rec.SrcPort = int(payload.Get("connection.src_port").Int())
	} else {
		rec.SrcPort = int(payload.Get("src_port").Int())
	}
	if payload.Get("connection.dest_port").Exists() {
		rec.DstPort = int(payload.Get("connection.dest_port").Int())
	} else {
		rec.DstPort = int(payload.Get("dest_port").Int())
	}
	if payload.Get("connection.protocol").Exists() {
		rec.Proto = toProto(payload.Get("connection.protocol").Value())
	} else {
		rec.Proto = toProto(payload.Get("protocol").Value())
	}
	rec.BytesFwd = toInt64(payload.Get("bytes_sent").Value())
	rec.BytesBwd = toInt64(payload.Get("bytes_received").Value())
	rec.PktsFwd = toInt64(payload.Get("packets_sent").Value())
	rec.PktsBwd = toInt64(payload.Get("packets_received").Value())
	if rec.BytesFwd > 0 || rec.BytesBwd > 0 || rec.PktsFwd > 0 || rec.PktsBwd > 0 {
		rec.MetricsKnown = true
	}
	rec.TenantID = firstNonEmpty(
		root.Get("resource.labels.project_id").String(),
		payload.Get("src_instance.project_id").String(),
		payload.Get("dest_instance.project_id").String(),
		payload.Get("src_vpc.project_id").String(),
		payload.Get("dest_vpc.project_id").String(),
	)
	rec.SourceID = firstNonEmpty(
		root.Get("resource.labels.subnetwork_name").String(),
		payload.Get("src_vpc.subnetwork_name").String(),
		payload.Get("dest_vpc.subnetwork_name").String(),
	)
	rec.SourceEventID = firstNonEmpty(
		root.Get("insertId").String(),
		root.Get("operation.id").String(),
		payload.Get("source_event_id").String(),
		payload.Get("event_id").String(),
	)
	rec.TraceID = firstNonEmpty(
		root.Get("trace").String(),
		root.Get("operation.id").String(),
		payload.Get("trace_id").String(),
	)
	rec.Verdict = strings.ToUpper(payload.Get("reporter").String())
	if rec.SrcIP == "" || rec.DstIP == "" || rec.TenantID == "" {
		return nil, errors.New("missing required fields")
	}
	return []model.Record{rec}, nil
}

func parseTimestamp(value any) int64 {
	switch v := value.(type) {
	case string:
		if v == "" {
			return 0
		}
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			t, err = time.Parse(time.RFC3339, v)
			if err != nil {
				return 0
			}
		}
		return t.Unix()
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func toInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		if v == "" {
			return 0
		}
		var out int64
		for _, ch := range v {
			if ch < '0' || ch > '9' {
				return 0
			}
			out = out*10 + int64(ch-'0')
		}
		return out
	default:
		return 0
	}
}

func toProto(value any) int {
	switch v := value.(type) {
	case string:
		switch strings.ToUpper(strings.TrimSpace(v)) {
		case "TCP", "T":
			return 6
		case "UDP", "U":
			return 17
		case "ICMP":
			return 1
		}
		return 0
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
