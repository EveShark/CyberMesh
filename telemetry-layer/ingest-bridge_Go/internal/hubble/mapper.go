package hubble

import (
	"errors"
	"time"

	"cybermesh/telemetry-layer/ingest-bridge/internal/model"
	"github.com/cilium/cilium/api/v1/flow"
)

func ToFlowEvent(src *flow.Flow, tenantID string) (model.FlowEvent, error) {
	if src == nil {
		return model.FlowEvent{}, errors.New("nil flow")
	}
	ip := src.GetIP()
	if ip == nil || ip.GetSource() == "" || ip.GetDestination() == "" {
		return model.FlowEvent{}, errors.New("missing ip")
	}
	l4 := src.GetL4()
	if l4 == nil {
		return model.FlowEvent{}, errors.New("missing l4")
	}

	srcPort, dstPort, proto, err := parsePorts(l4)
	if err != nil {
		return model.FlowEvent{}, err
	}

	ts := time.Now().Unix()
	if t := src.GetTime(); t != nil {
		ts = t.AsTime().Unix()
	}

	bytes, packets := extractMetrics(src)
	dir := mapDirection(src)

	identity := model.Identity{}
	if endpoint := src.GetSource(); endpoint != nil {
		identity.Namespace = endpoint.GetNamespace()
		identity.Pod = endpoint.GetPodName()
	}
	if node := src.GetNodeName(); node != "" {
		identity.Node = node
	}

	return model.FlowEvent{
		Timestamp:    ts,
		TenantID:     tenantID,
		SrcIP:        ip.GetSource(),
		DstIP:        ip.GetDestination(),
		SrcPort:      srcPort,
		DstPort:      dstPort,
		Proto:        proto,
		Bytes:   bytes,
		Packets: packets,
		Direction:    dir,
		Identity:     identity,
		Verdict:      src.GetVerdict().String(),
		// Mark counters as known when we have at least packet-level observations.
		// For Hubble this is derived from event counts (see extractMetrics).
		MetricsKnown: packets > 0,
	}, nil
}

func parsePorts(l4 *flow.Layer4) (int, int, int, error) {
	if tcp := l4.GetTCP(); tcp != nil {
		return int(tcp.GetSourcePort()), int(tcp.GetDestinationPort()), 6, nil
	}
	if udp := l4.GetUDP(); udp != nil {
		return int(udp.GetSourcePort()), int(udp.GetDestinationPort()), 17, nil
	}
	if sctp := l4.GetSCTP(); sctp != nil {
		return int(sctp.GetSourcePort()), int(sctp.GetDestinationPort()), 132, nil
	}
	if icmp := l4.GetICMPv4(); icmp != nil {
		return 0, 0, 1, nil
	}
	if icmp := l4.GetICMPv6(); icmp != nil {
		return 0, 0, 58, nil
	}
	return 0, 0, 0, errors.New("unsupported l4 protocol")
}

func mapDirection(src *flow.Flow) string {
	switch src.GetTrafficDirection() {
	case flow.TrafficDirection_INGRESS:
		return "ingress"
	case flow.TrafficDirection_EGRESS:
		return "egress"
	default:
		if isReply := src.GetIsReply(); isReply != nil && isReply.GetValue() {
			return "inbound"
		}
		if src.GetReply() {
			return "inbound"
		}
		return "outbound"
	}
}

func extractMetrics(src *flow.Flow) (int64, int64) {
	// Hubble Flow events are not aggregated flow records; they generally do not carry
	// byte/packet counters. For Phase-1/2 telemetry we need *some* monotonic signal
	// so the downstream aggregator can compute rates over a window.
	//
	// Strategy:
	// - Treat each received Flow event as one "packet-like" observation.
	// - Bytes are unknown: keep 0 (do not invent payload size).
	//
	// This is intentionally conservative and keeps "metrics_known" semantics honest
	// at higher layers (they can choose to treat these as derived counts).
	if src == nil {
		return 0, 0
	}
	return 0, 1
}
