package ack

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	pb "backend/proto"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
)

// kafkaGoWriter is the tiny surface we need from a Kafka writer.
// It enables unit tests without requiring a broker.
type kafkaGoWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// KafkaGoPublisher implements Publisher using a kafka-go style writer.
// This avoids Sarama producer lifecycle edge cases while keeping the consumer on Sarama.
type KafkaGoPublisher struct {
	writer       kafkaGoWriter
	logger       *zap.Logger
	metrics      *metrics.Recorder
	retryMax     int
	retryBackoff time.Duration
	topic        string
	signer       Signer
}

type KafkaGoOptions struct {
	Writer       kafkaGoWriter
	Topic        string
	RetryMax     int
	RetryBackoff time.Duration
	Logger       *zap.Logger
	Metrics      *metrics.Recorder
	Signer       Signer
}

func NewKafkaGoPublisher(opts KafkaGoOptions) (*KafkaGoPublisher, error) {
	if opts.Writer == nil {
		return nil, fmt.Errorf("ack publisher: writer required")
	}
	if opts.Topic == "" {
		return nil, fmt.Errorf("ack publisher: topic required")
	}
	retryMax := opts.RetryMax
	if retryMax <= 0 {
		retryMax = 5
	}
	retryBackoff := opts.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = 500 * time.Millisecond
	}
	return &KafkaGoPublisher{
		writer:       opts.Writer,
		logger:       opts.Logger,
		metrics:      opts.Metrics,
		retryMax:     retryMax,
		retryBackoff: retryBackoff,
		topic:        opts.Topic,
		signer:       opts.Signer,
	}, nil
}

func (p *KafkaGoPublisher) Publish(ctx context.Context, payload Payload) error {
	ack := &pb.PolicyAckEvent{
		PolicyId:           payload.Event.Spec.ID,
		ScopeIdentifier:    payload.Scope,
		Tenant:             payload.Tenant,
		Region:             payload.Region,
		Result:             string(payload.Result),
		Reason:             payload.Reason,
		ErrorCode:          payload.ErrorCode,
		AppliedAt:          payload.AppliedAt.Unix(),
		AckedAt:            payload.AckedAt.Unix(),
		QcReference:        payload.QCRef,
		ControllerInstance: payload.Controller,
		FastPath:           payload.FastPath,
		RuleHash:           append([]byte(nil), payload.RuleHash...),
		ProducerId:         append([]byte(nil), payload.ProducerID...),
	}
	msgBytes, err := proto.Marshal(ack)
	if err != nil {
		p.logError("marshal ack payload", payload.Event.Spec.ID, err)
		return fmt.Errorf("ack publish: marshal payload: %w", err)
	}

	headers := make([]kafka.Header, 0, 2)
	if p.signer != nil {
		out, serr := p.signer.Sign(ctx, payload, msgBytes)
		if serr != nil {
			p.logError("sign ack payload", payload.Event.Spec.ID, serr)
			return fmt.Errorf("ack publish: sign payload: %w", serr)
		}
		if len(out.Signature) > 0 {
			headers = append(headers, kafka.Header{Key: "ack-signature", Value: out.Signature})
			if out.Algorithm != "" {
				headers = append(headers, kafka.Header{Key: "ack-signature-alg", Value: []byte(out.Algorithm)})
			}
		}
	}

	key := []byte(payload.Event.Spec.ID)
	msg := kafka.Message{
		Key:     append([]byte(nil), key...),
		Value:   append([]byte(nil), msgBytes...),
		Headers: headers,
	}

	var lastErr error
	backoff := p.retryBackoff
	for attempt := 0; attempt < p.retryMax; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := p.writer.WriteMessages(ctx, msg); err == nil {
			if p.metrics != nil {
				p.metrics.ObserveAckPublish("success")
			}
			return nil
		} else {
			lastErr = err
		}
		if p.metrics != nil {
			p.metrics.ObserveAckRetry()
		}
		time.Sleep(backoff)
		backoff *= 2
	}
	p.logError("publish ack", payload.Event.Spec.ID, lastErr)
	if p.metrics != nil {
		p.metrics.ObserveAckPublish("failure")
	}
	return fmt.Errorf("ack publish: %w", lastErr)
}

func (p *KafkaGoPublisher) PublishBatch(ctx context.Context, payloads []Payload) error {
	var firstErr error
	for _, payload := range payloads {
		if err := p.Publish(ctx, payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *KafkaGoPublisher) Close(ctx context.Context) error {
	_ = ctx
	return p.writer.Close()
}

func (p *KafkaGoPublisher) logError(msg, policyID string, err error) {
	if p.logger != nil {
		p.logger.Error(msg, zap.String("policy_id", policyID), zap.Error(err))
	}
}
