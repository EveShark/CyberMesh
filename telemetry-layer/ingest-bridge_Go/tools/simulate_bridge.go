package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"cybermesh/telemetry-layer/ingest-bridge/internal/aggregate"
	"cybermesh/telemetry-layer/ingest-bridge/internal/model"
	"cybermesh/telemetry-layer/ingest-bridge/internal/validate"
)

func main() {
	now := time.Now().Unix()
	agg := aggregate.New(10)

	events := []model.FlowEvent{
		{
			Timestamp: now - 12,
			TenantID:  "default",
			SrcIP:     "10.0.0.1",
			DstIP:     "10.0.0.2",
			SrcPort:   12345,
			DstPort:   443,
			Proto:     6,
			Bytes:     12000,
			Packets:   15,
			Direction: "egress",
		},
		{
			Timestamp: now - 12,
			TenantID:  "default",
			SrcIP:     "10.0.0.2",
			DstIP:     "10.0.0.1",
			SrcPort:   443,
			DstPort:   12345,
			Proto:     6,
			Bytes:     3400,
			Packets:   8,
			Direction: "ingress",
		},
		{
			Timestamp: now - 12,
			TenantID:  "default",
			SrcIP:     "2001:db8::1",
			DstIP:     "2001:db8::2",
			SrcPort:   53,
			DstPort:   5353,
			Proto:     17,
			Bytes:     500,
			Packets:   4,
			Direction: "egress",
		},
		{
			Timestamp: now - 12,
			TenantID:  "default",
			SrcIP:     "999.1.1.1",
			DstIP:     "10.0.0.9",
			SrcPort:   80,
			DstPort:   80,
			Proto:     6,
			Bytes:     10,
			Packets:   1,
			Direction: "egress",
		},
		{
			Timestamp: now - 12,
			TenantID:  "default",
			SrcIP:     "10.0.0.10",
			DstIP:     "10.0.0.11",
			SrcPort:   -1,
			DstPort:   70000,
			Proto:     6,
			Bytes:     1,
			Packets:   1,
			Direction: "egress",
		},
		{
			Timestamp: now - 12,
			TenantID:  "default",
			SrcIP:     "10.0.0.12",
			DstIP:     "10.0.0.13",
			SrcPort:   22,
			DstPort:   22,
			Proto:     -1,
			Bytes:     1,
			Packets:   1,
			Direction: "egress",
		},
		{
			Timestamp: now - 12,
			TenantID:  "",
			SrcIP:     "10.0.0.3",
			DstIP:     "10.0.0.4",
			SrcPort:   1,
			DstPort:   1,
			Proto:     6,
			Bytes:     1,
			Packets:   1,
			Direction: "egress",
		},
		{
			Timestamp: now - 25,
			TenantID:  "default",
			SrcIP:     "10.0.0.20",
			DstIP:     "10.0.0.21",
			SrcPort:   8080,
			DstPort:   80,
			Proto:     6,
			Bytes:     100,
			Packets:   2,
			Direction: "egress",
		},
		{
			Timestamp: now - 5,
			TenantID:  "default",
			SrcIP:     "10.0.0.20",
			DstIP:     "10.0.0.21",
			SrcPort:   8080,
			DstPort:   80,
			Proto:     6,
			Bytes:     200,
			Packets:   3,
			Direction: "egress",
		},
	}

	for _, ev := range events {
		agg.Add(ev)
	}

	out := agg.Flush(time.Now())
	encoder := json.NewEncoder(os.Stdout)
	valid := 0
	invalid := 0

	for _, flow := range out {
		if err := validate.FlowAggregate(flow); err != nil {
			invalid++
			fmt.Fprintf(os.Stdout, "invalid: %v\n", err)
			continue
		}
		valid++
		_ = encoder.Encode(flow)
	}

	fmt.Fprintf(os.Stdout, "valid=%d invalid=%d\n", valid, invalid)
}
