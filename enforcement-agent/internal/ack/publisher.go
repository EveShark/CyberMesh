package ack

import (
	"context"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
	pb "backend/proto"
)

type syncProducer interface {
	SendMessage(*sarama.ProducerMessage) (partition int32, offset int64, err error)
	Close() error
}

// Result enumerates ACK results.
type Result string

const (
	ResultApplied Result = "applied"
	ResultFailed  Result = "failed"
)

// Payload contains data for ACK publication.
type Payload struct {
	Event      policy.Event
	Result     Result
	Reason     string
	ErrorCode  string
	AppliedAt  time.Time
	AckedAt    time.Time
	FastPath   bool
	Scope      string
	Tenant     string
	Region     string
	QCRef      string
	Controller string
	RuleHash   []byte
	ProducerID []byte
}

// Publisher defines ACK emission contract.
type Publisher interface {
	Publish(ctx context.Context, payload Payload) error
	PublishBatch(ctx context.Context, payloads []Payload) error
	Close(ctx context.Context) error
}

// SignOutput carries signature material for ACK payloads.
type SignOutput struct {
	Signature []byte
	Algorithm string
}

// Signer decorates ACK payloads with signatures before publish.
type Signer interface {
	Sign(ctx context.Context, payload Payload, encoded []byte) (SignOutput, error)
}

// KafkaPublisher implements Publisher backed by Kafka.
type KafkaPublisher struct {
	producer     syncProducer
	logger       *zap.Logger
	metrics      *metrics.Recorder
	retryMax     int
	retryBackoff time.Duration
	topic        string
	signer       Signer
}

// Options captures publisher configuration.
type Options struct {
	Producer     syncProducer
	Topic        string
	RetryMax     int
	RetryBackoff time.Duration
	Logger       *zap.Logger
	Metrics      *metrics.Recorder
	Signer       Signer
}

// NewKafkaPublisher builds a publisher.
func NewKafkaPublisher(opts Options) (*KafkaPublisher, error) {
	if opts.Producer == nil {
		return nil, fmt.Errorf("ack publisher: producer required")
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
	return &KafkaPublisher{
		producer:     opts.Producer,
		logger:       opts.Logger,
		metrics:      opts.Metrics,
		retryMax:     retryMax,
		retryBackoff: retryBackoff,
		topic:        opts.Topic,
		signer:       opts.Signer,
	}, nil
}

// Publish enqueues ACK payload.
func (p *KafkaPublisher) Publish(ctx context.Context, payload Payload) error {
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
	key := payload.Event.Spec.ID
	kmsg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(msgBytes),
	}
	if p.signer != nil {
		out, serr := p.signer.Sign(ctx, payload, msgBytes)
		if serr != nil {
			p.logError("sign ack payload", payload.Event.Spec.ID, serr)
			return fmt.Errorf("ack publish: sign payload: %w", serr)
		}
		if len(out.Signature) > 0 {
			kmsg.Headers = append(kmsg.Headers, sarama.RecordHeader{Key: []byte("ack-signature"), Value: out.Signature})
			if out.Algorithm != "" {
				kmsg.Headers = append(kmsg.Headers, sarama.RecordHeader{Key: []byte("ack-signature-alg"), Value: []byte(out.Algorithm)})
			}
		}
	}
	var lastErr error
	backoff := p.retryBackoff
	for attempt := 0; attempt < p.retryMax; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, _, err = p.producer.SendMessage(kmsg)
		if err == nil {
			if p.metrics != nil {
				p.metrics.ObserveAckPublish("success")
			}
			return nil
		}
		lastErr = err
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

// PublishBatch emits multiple payloads sequentially.
func (p *KafkaPublisher) PublishBatch(ctx context.Context, payloads []Payload) error {
	var firstErr error
	for _, payload := range payloads {
		if err := p.Publish(ctx, payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Close flushes producer.
func (p *KafkaPublisher) Close(ctx context.Context) error {
	return p.producer.Close()
}

func (p *KafkaPublisher) logError(msg, policyID string, err error) {
	if p.logger != nil {
		p.logger.Error(msg, zap.String("policy_id", policyID), zap.Error(err))
	}
}
