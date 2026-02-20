package derive

import "cybermesh/telemetry-layer/stream-processor/internal/model"

func ApplyTimingDerivation(flow *model.FlowAggregate, policy string) bool {
	if flow == nil {
		return false
	}
	switch policy {
	case "approx_iat":
		return applyApproxIAT(flow)
	default:
		return false
	}
}

func applyApproxIAT(flow *model.FlowAggregate) bool {
	if flow.TimingKnown || flow.DurationMS <= 0 {
		return false
	}
	totalPkts := flow.PktsFwd + flow.PktsBwd
	if totalPkts <= 1 {
		return false
	}
	mean := float64(flow.DurationMS) / float64(totalPkts-1)
	flow.FlowIatMean = mean
	flow.FlowIatMin = mean
	flow.FlowIatMax = mean
	flow.FlowIatStd = 0.0
	flow.TimingKnown = true
	flow.TimingDerived = true
	flow.DerivationPolicy = "approx_iat"
	return true
}
