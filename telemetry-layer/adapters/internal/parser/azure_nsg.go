package parser

import (
	"errors"
	"strings"

	"cybermesh/telemetry-layer/adapters/internal/model"
	"github.com/tidwall/gjson"
)

func ParseAzureNSGFlowTuples(line []byte) ([]model.Record, error) {
	if len(line) == 0 {
		return nil, errors.New("empty input")
	}
	root := gjson.ParseBytes(line)
	resourceID := firstNonEmpty(
		root.Get("resourceId").String(),
		root.Get("records.0.resourceId").String(),
	)
	tenantID := firstNonEmpty(
		root.Get("tenant_id").String(),
		root.Get("records.0.tenant_id").String(),
	)
	if tenantID == "" {
		tenantID = extractSubscription(resourceID)
	}
	sourceID := extractNSG(resourceID)
	paths := []string{
		"records.#.properties.flows.#.flows.#.flowTuples.#",
		"records.#.properties.flows.#.flows.#.flowTuples",
		"records.#.properties.flows.#.flowTuples.#",
		"records.#.properties.flows.#.flowTuples",
	}
	var records []model.Record
	found := false
	for _, path := range paths {
		tuples := root.Get(path)
		if !tuples.Exists() {
			continue
		}
		found = true
		if tuples.IsArray() {
			tuples.ForEach(func(_, v gjson.Result) bool {
				rec, err := parseAzureTuple(v.String(), tenantID, sourceID)
				if err == nil {
					records = append(records, rec)
				}
				return true
			})
		} else {
			rec, err := parseAzureTuple(tuples.String(), tenantID, sourceID)
			if err == nil {
				records = append(records, rec)
			}
		}
	}
	if !found {
		return nil, errors.New("missing flowTuples")
	}
	if len(records) == 0 {
		return nil, errors.New("no valid flow tuples")
	}
	return records, nil
}

func parseAzureTuple(value string, tenantID string, sourceID string) (model.Record, error) {
	parts := strings.Split(value, ",")
	if len(parts) < 8 {
		return model.Record{}, errors.New("invalid tuple")
	}
	rec := model.Record{
		SourceType: "azure_nsg",
		SourceID:   sourceID,
		TenantID:   tenantID,
		Timestamp:  parseInt64(parts[0]),
		SrcIP:      parts[1],
		DstIP:      parts[2],
		SrcPort:    parseInt(parts[3]),
		DstPort:    parseInt(parts[4]),
		Proto:      parseProto(parts[5]),
		Verdict:    mapDecision(parts[7]),
	}
	if len(parts) > 9 {
		rec.PktsFwd = parseInt64(parts[9])
	}
	if len(parts) > 10 {
		rec.BytesFwd = parseInt64(parts[10])
	}
	if len(parts) > 11 {
		rec.PktsBwd = parseInt64(parts[11])
	}
	if len(parts) > 12 {
		rec.BytesBwd = parseInt64(parts[12])
	}
	if rec.PktsFwd > 0 || rec.PktsBwd > 0 || rec.BytesFwd > 0 || rec.BytesBwd > 0 {
		rec.MetricsKnown = true
	}
	if rec.TenantID == "" || rec.SrcIP == "" || rec.DstIP == "" {
		return model.Record{}, errors.New("missing required fields")
	}
	return rec, nil
}

func mapDecision(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "A", "ALLOW":
		return "ALLOW"
	case "D", "DENY":
		return "DENY"
	default:
		return strings.ToUpper(value)
	}
}

func extractSubscription(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "subscriptions") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractNSG(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "networkSecurityGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
