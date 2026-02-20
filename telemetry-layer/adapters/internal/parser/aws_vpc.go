package parser

import (
	"errors"
	"strings"

	"cybermesh/telemetry-layer/adapters/internal/model"
)

func ParseAWSVPCText(line string) ([]model.Record, error) {
	raw := strings.TrimSpace(line)
	if raw == "" {
		return nil, errors.New("empty input")
	}
	fields := strings.Fields(raw)
	if len(fields) < 14 {
		return nil, errors.New("invalid vpc flow log line")
	}
	// version := fields[0]
	accountID := fields[1]
	// interfaceID := fields[2]
	src := fields[3]
	dst := fields[4]
	srcPort := parseInt(fields[5])
	dstPort := parseInt(fields[6])
	proto := parseProto(fields[7])
	pkts := parseInt64(fields[8])
	bytes := parseInt64(fields[9])
	start := parseInt64(fields[10])
	// end := fields[11]
	action := fields[12]
	// logStatus := fields[13]
	rec := model.Record{
		Timestamp:    start,
		TenantID:     accountID,
		SrcIP:        src,
		DstIP:        dst,
		SrcPort:      srcPort,
		DstPort:      dstPort,
		Proto:        proto,
		PktsFwd:      pkts,
		BytesFwd:     bytes,
		MetricsKnown: pkts > 0 || bytes > 0,
		Verdict:      strings.ToUpper(action),
		SourceType:   "aws_vpc",
	}
	if rec.TenantID == "" || rec.SrcIP == "" || rec.DstIP == "" {
		return nil, errors.New("missing required fields")
	}
	return []model.Record{rec}, nil
}

func parseInt(value string) int {
	if value == "-" || value == "" {
		return 0
	}
	n := 0
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func parseInt64(value string) int64 {
	if value == "-" || value == "" {
		return 0
	}
	var n int64
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int64(ch-'0')
	}
	return n
}

func parseProto(value string) int {
	if value == "-" || value == "" {
		return 0
	}
	switch strings.ToUpper(value) {
	case "TCP":
		return 6
	case "UDP":
		return 17
	case "ICMP":
		return 1
	}
	return parseInt(value)
}
