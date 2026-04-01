package controller

import (
	"context"

	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

type FastHandler struct {
	trust   *policy.TrustedKeys
	ctrl    *Controller
	metrics *metrics.Recorder
	rejects FastRejectSink
	logger  *zap.Logger
}

func NewFastHandler(trust *policy.TrustedKeys, ctrl *Controller, recorder *metrics.Recorder, rejects FastRejectSink, logger *zap.Logger) *FastHandler {
	if rejects == nil {
		rejects = noopFastRejectSink{}
	}
	return &FastHandler{trust: trust, ctrl: ctrl, metrics: recorder, rejects: rejects, logger: logger}
}

func (h *FastHandler) HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	if h == nil || h.ctrl == nil {
		return nil
	}
	evt, err := h.trust.VerifyAndParseFast(msg.Value)
	if err != nil {
		if h.metrics != nil {
			h.metrics.ObserveRejected()
			h.metrics.ObserveKafkaError("fast_handler")
		}
		if h.rejects != nil {
			if pubErr := h.rejects.PublishReject(ctx, msg, err); pubErr != nil && h.logger != nil {
				h.logger.Warn("fast mitigation reject artifact publish failed", zap.Error(pubErr))
			}
		}
		return nil
	}
	return h.ctrl.HandleFastEvent(ctx, evt)
}
