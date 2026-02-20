package policyack

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"backend/pkg/utils"
	pb "backend/proto"
	"github.com/IBM/sarama"
	"google.golang.org/protobuf/proto"
)

type Consumer struct {
	cfg    Config
	store  *Store
	trust  *TrustedKeys
	log    *utils.Logger
	audit  *utils.AuditLogger
	dlqProducer sarama.SyncProducer
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	cg sarama.ConsumerGroup
}

type Options struct {
	Config Config
	Store  *Store
	Trust  *TrustedKeys
	Logger *utils.Logger
	Audit  *utils.AuditLogger
	DLQ    sarama.SyncProducer
}

func New(ctx context.Context, group sarama.ConsumerGroup, opts Options) (*Consumer, error) {
	if group == nil {
		return nil, fmt.Errorf("policy ack consumer: consumer group required")
	}
	if !opts.Config.Enabled {
		return nil, fmt.Errorf("policy ack consumer: disabled")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("policy ack consumer: store required")
	}
	if opts.Config.SigningRequired && opts.Trust == nil {
		return nil, fmt.Errorf("policy ack consumer: signing required but trust store is nil")
	}

	cctx, cancel := context.WithCancel(ctx)
	return &Consumer{
		cfg:    opts.Config,
		store:  opts.Store,
		trust:  opts.Trust,
		log:    opts.Logger,
		audit:  opts.Audit,
		dlqProducer: opts.DLQ,
		ctx:    cctx,
		cancel: cancel,
		cg:     group,
	}, nil
}

func (c *Consumer) Start() error {
	if c == nil {
		return fmt.Errorf("policy ack consumer: nil")
	}
	c.wg.Add(1)
	go c.loop()
	return nil
}

func (c *Consumer) Stop() error {
	if c == nil {
		return nil
	}
	c.cancel()
	c.wg.Wait()
	if c.dlqProducer != nil {
		_ = c.dlqProducer.Close()
	}
	if c.cg != nil {
		return c.cg.Close()
	}
	return nil
}

func (c *Consumer) loop() {
	defer c.wg.Done()

	handler := &handler{c: c}
	for {
		if err := c.cg.Consume(c.ctx, []string{c.cfg.Topic}, handler); err != nil {
			if errors.Is(err, sarama.ErrClosedConsumerGroup) {
				return
			}
			if c.log != nil {
				c.log.ErrorContext(c.ctx, "policy ack consumer error, retrying",
					utils.ZapError(err),
					utils.ZapString("topic", c.cfg.Topic))
			}
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		if c.ctx.Err() != nil {
			return
		}
	}
}

type handler struct {
	c *Consumer
}

func (h *handler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *handler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *handler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case <-sess.Context().Done():
			return nil
		case msg := <-claim.Messages():
			if msg == nil {
				return nil
			}
			if err := h.c.process(sess.Context(), msg); err != nil {
				// Permanent errors are DLQ'ed and marked. Transient errors return error to trigger retry.
				if errors.Is(err, errPermanent) {
					sess.MarkMessage(msg, "")
					continue
				}
				return err
			}
			sess.MarkMessage(msg, "")
		}
	}
}

var errPermanent = errors.New("permanent")

func (c *Consumer) process(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var ack pb.PolicyAckEvent
	if err := proto.Unmarshal(msg.Value, &ack); err != nil {
		c.sendDLQ(ctx, msg, "unmarshal_failed", err)
		return fmt.Errorf("%w: policy ack unmarshal: %v", errPermanent, err)
	}

	if strings.TrimSpace(ack.PolicyId) == "" || strings.TrimSpace(ack.ControllerInstance) == "" {
		c.sendDLQ(ctx, msg, "invalid_required_fields", fmt.Errorf("policy_id/controller_instance required"))
		return fmt.Errorf("%w: invalid required fields", errPermanent)
	}

	res := strings.ToLower(strings.TrimSpace(ack.Result))
	if res != "applied" && res != "failed" {
		c.sendDLQ(ctx, msg, "invalid_result", fmt.Errorf("result=%q", ack.Result))
		return fmt.Errorf("%w: invalid result", errPermanent)
	}
	ack.Result = res

	sig := header(msg.Headers, "ack-signature")
	algo := strings.ToLower(string(header(msg.Headers, "ack-signature-alg")))
	if len(sig) == 0 {
		if c.cfg.SigningRequired {
			c.sendDLQ(ctx, msg, "missing_signature", fmt.Errorf("ack-signature header missing"))
			return fmt.Errorf("%w: signature required", errPermanent)
		}
	} else {
		if algo != "" && algo != "ed25519" {
			c.sendDLQ(ctx, msg, "unsupported_signature_alg", fmt.Errorf("alg=%q", algo))
			return fmt.Errorf("%w: unsupported signature alg", errPermanent)
		}
		if c.trust == nil || !c.trust.Verify(msg.Value, sig) {
			c.sendDLQ(ctx, msg, "signature_verify_failed", fmt.Errorf("verify failed"))
			return fmt.Errorf("%w: signature verify failed", errPermanent)
		}
	}

	// Store with bounded retry for transient DB errors.
	backoff := c.cfg.StoreRetryBackoff
	var last error
	for attempt := 1; attempt <= c.cfg.StoreRetryMax; attempt++ {
		if err := c.store.Upsert(ctx, &ack, time.Now().UTC()); err != nil {
			last = err
			if c.log != nil {
				c.log.WarnContext(ctx, "policy ack store failed; retrying",
					utils.ZapInt("attempt", attempt),
					utils.ZapDuration("backoff", backoff),
					utils.ZapString("policy_id", ack.PolicyId),
					utils.ZapError(err))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			continue
		}
		if c.audit != nil {
			_ = c.audit.Info("policy_ack_persisted", map[string]interface{}{
				"policy_id":            ack.PolicyId,
				"controller_instance":  ack.ControllerInstance,
				"result":               ack.Result,
				"error_code":           ack.ErrorCode,
				"fast_path":            ack.FastPath,
				"ack_topic":            c.cfg.Topic,
				"consumer_group":       c.cfg.GroupID,
				"partition":            msg.Partition,
				"offset":               msg.Offset,
				"signature_present":    len(sig) > 0,
				"signature_alg":        algo,
			})
		}
		return nil
	}

	// Exhausted retries: treat as transient so we don't drop data.
	if last != nil {
		return fmt.Errorf("policy ack store retries exhausted: %w", last)
	}
	return fmt.Errorf("policy ack store retries exhausted")
}

func header(headers []*sarama.RecordHeader, key string) []byte {
	for _, h := range headers {
		if h == nil || len(h.Key) == 0 {
			continue
		}
		if string(h.Key) == key {
			return h.Value
		}
	}
	return nil
}

func (c *Consumer) sendDLQ(ctx context.Context, msg *sarama.ConsumerMessage, reason string, err error) {
	if c.audit != nil {
		_ = c.audit.Warn("policy_ack_rejected", map[string]interface{}{
			"reason":    reason,
			"error":     errString(err),
			"topic":     msg.Topic,
			"partition": msg.Partition,
			"offset":    msg.Offset,
		})
	}

	if c.dlqProducer == nil || c.cfg.DLQ == "" {
		if c.log != nil {
			c.log.WarnContext(ctx, "policy ack rejected (no dlq configured)",
				utils.ZapString("reason", reason),
				utils.ZapError(err))
		}
		return
	}

	headers := []sarama.RecordHeader{
		{Key: []byte("type"), Value: []byte("policy_ack")},
		{Key: []byte("reason"), Value: []byte(reason)},
		{Key: []byte("original_topic"), Value: []byte(msg.Topic)},
		{Key: []byte("original_partition"), Value: []byte(fmt.Sprintf("%d", msg.Partition))},
		{Key: []byte("original_offset"), Value: []byte(fmt.Sprintf("%d", msg.Offset))},
	}
	if err != nil {
		headers = append(headers, sarama.RecordHeader{Key: []byte("error"), Value: []byte(err.Error())})
	}

	key := sarama.StringEncoder(fmt.Sprintf("ack:%s:%d:%d", msg.Topic, msg.Partition, msg.Offset))
	dlqMsg := &sarama.ProducerMessage{
		Topic:   c.cfg.DLQ,
		Key:     key,
		Value:   sarama.ByteEncoder(msg.Value),
		Headers: headers,
	}
	if _, _, perr := c.dlqProducer.SendMessage(dlqMsg); perr != nil && c.log != nil {
		c.log.WarnContext(ctx, "failed to publish policy ack dlq",
			utils.ZapString("dlq_topic", c.cfg.DLQ),
			utils.ZapError(perr))
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
