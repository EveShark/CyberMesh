package validate

import (
	"errors"
	"net"

	"cybermesh/telemetry-layer/stream-processor/internal/model"
)

var (
	ErrMissingField = errors.New("missing required field")
	ErrInvalidIP    = errors.New("invalid ip")
	ErrInvalidPort  = errors.New("invalid port")
	ErrInvalidProto = errors.New("invalid proto")
)

func FlowAggregate(agg model.FlowAggregate) error {
	if agg.Schema == "" || agg.Schema != "flow.v1" {
		return ErrMissingField
	}
	if agg.Timestamp <= 0 || agg.TenantID == "" || agg.FlowID == "" {
		return ErrMissingField
	}
	if net.ParseIP(agg.SrcIP) == nil || net.ParseIP(agg.DstIP) == nil {
		return ErrInvalidIP
	}
	if agg.SrcPort < 0 || agg.SrcPort > 65535 || agg.DstPort < 0 || agg.DstPort > 65535 {
		return ErrInvalidPort
	}
	if agg.Proto < 0 {
		return ErrInvalidProto
	}
	return nil
}
