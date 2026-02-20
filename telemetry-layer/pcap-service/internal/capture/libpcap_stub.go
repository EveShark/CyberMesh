//go:build !pcap

package capture

import (
	"context"
	"errors"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
)

func captureLibpcap(_ context.Context, _ Config, _ *telemetrypb.PcapRequestV1, _ int64) (Result, error) {
	return Result{}, errors.New("libpcap capture not enabled (build with -tags=pcap)")
}
