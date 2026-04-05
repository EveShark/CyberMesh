package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
)

const (
	defaultCommandRejectQueueSize = 256
	defaultCommandRejectWorkers   = 1
)

type CommandRejectSink interface {
	PublishReject(ctx context.Context, msg *sarama.ConsumerMessage, reason error) error
	Close() error
}

type noopCommandRejectSink struct{}

func (noopCommandRejectSink) PublishReject(context.Context, *sarama.ConsumerMessage, error) error {
	return nil
}
func (noopCommandRejectSink) Close() error { return nil }

type commandRejectRecord struct {
	SchemaVersion   string `json:"schema_version"`
	ConsumerTopic   string `json:"consumer_topic"`
	ConsumerGroup   string `json:"consumer_group,omitempty"`
	Partition       int32  `json:"partition"`
	Offset          int64  `json:"offset"`
	MessageKeyB64   string `json:"message_key_b64,omitempty"`
	MessageValueB64 string `json:"message_value_b64"`
	MessageTimeUnix int64  `json:"message_time_unix,omitempty"`
	RejectedAtUnix  int64  `json:"rejected_at_unix"`
	Reason          string `json:"reason"`
}

type queuedCommandReject struct {
	record []byte
}

type kafkaCommandRejectSink struct {
	producer syncProducer
	topic    string
	groupID  string
	logger   *zap.Logger

	queue   chan queuedCommandReject
	closing chan struct{}
	wg      sync.WaitGroup
	closed  atomic.Bool
	rejects atomic.Int64
}

func NewKafkaCommandRejectSink(cfg sarama.Config, brokers []string, topic, groupID string, logger *zap.Logger) (CommandRejectSink, error) {
	if topic == "" {
		return noopCommandRejectSink{}, nil
	}
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true
	cfg.Producer.RequiredAcks = sarama.WaitForLocal
	cfg.Producer.Retry.Max = 0
	cfg.Producer.Timeout = 3 * time.Second
	cfg.Net.DialTimeout = 3 * time.Second
	cfg.Net.ReadTimeout = 3 * time.Second
	cfg.Net.WriteTimeout = 3 * time.Second

	producer, err := sarama.NewSyncProducer(brokers, &cfg)
	if err != nil {
		return nil, fmt.Errorf("create command reject producer: %w", err)
	}
	sink := &kafkaCommandRejectSink{
		producer: producer,
		topic:    topic,
		groupID:  groupID,
		logger:   logger,
		queue:    make(chan queuedCommandReject, defaultCommandRejectQueueSize),
		closing:  make(chan struct{}),
	}
	for i := 0; i < defaultCommandRejectWorkers; i++ {
		sink.wg.Add(1)
		go sink.worker()
	}
	return sink, nil
}

func (s *kafkaCommandRejectSink) PublishReject(ctx context.Context, msg *sarama.ConsumerMessage, reason error) error {
	if s == nil || s.producer == nil || msg == nil || reason == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.closed.Load() {
		return fmt.Errorf("command reject sink closed")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	payload, err := s.buildRecord(msg, reason)
	if err != nil {
		return err
	}
	item := queuedCommandReject{record: payload}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closing:
		return fmt.Errorf("command reject sink closing")
	case s.queue <- item:
		return nil
	default:
		s.rejects.Add(1)
		return fmt.Errorf("command reject queue full")
	}
}

func (s *kafkaCommandRejectSink) Close() error {
	if s == nil || s.producer == nil {
		return nil
	}
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(s.closing)
	s.wg.Wait()
	return s.producer.Close()
}

func (s *kafkaCommandRejectSink) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.closing:
			for {
				select {
				case item := <-s.queue:
					if err := s.publish(item.record); err != nil && s.logger != nil {
						s.logger.Warn("command reject publish failed", zap.Error(err), zap.String("topic", s.topic))
					}
				default:
					return
				}
			}
		case item := <-s.queue:
			if err := s.publish(item.record); err != nil && s.logger != nil {
				s.logger.Warn("command reject publish failed", zap.Error(err), zap.String("topic", s.topic))
			}
		}
	}
}

func (s *kafkaCommandRejectSink) publish(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	_, _, err := s.producer.SendMessage(&sarama.ProducerMessage{
		Topic: s.topic,
		Key:   sarama.StringEncoder(s.groupID),
		Value: sarama.ByteEncoder(payload),
	})
	return err
}

func (s *kafkaCommandRejectSink) buildRecord(msg *sarama.ConsumerMessage, reason error) ([]byte, error) {
	record := commandRejectRecord{
		SchemaVersion:   "control.enforcement_command.reject.v1",
		ConsumerTopic:   msg.Topic,
		ConsumerGroup:   s.groupID,
		Partition:       msg.Partition,
		Offset:          msg.Offset,
		MessageValueB64: base64.StdEncoding.EncodeToString(msg.Value),
		RejectedAtUnix:  time.Now().UTC().Unix(),
		Reason:          reason.Error(),
	}
	if len(msg.Key) > 0 {
		record.MessageKeyB64 = base64.StdEncoding.EncodeToString(msg.Key)
	}
	if !msg.Timestamp.IsZero() {
		record.MessageTimeUnix = msg.Timestamp.UTC().Unix()
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal command reject record: %w", err)
	}
	return payload, nil
}
