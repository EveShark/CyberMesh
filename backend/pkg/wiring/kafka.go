package wiring

import (
	"context"
)

// KafkaProducer defines the interface for publishing control messages to Kafka
// TODO: Implement actual Kafka integration for control.* topics
type KafkaProducer interface {
	// PublishBlockCommitted publishes a block commit event to control.blocks topic
	PublishBlockCommitted(ctx context.Context, height uint64, hash [32]byte, txCount int) error

	// PublishStateTransition publishes state changes to control.state topic
	PublishStateTransition(ctx context.Context, version uint64, root [32]byte) error

	// PublishValidationFailure publishes validation failures to control.alerts topic
	PublishValidationFailure(ctx context.Context, reason string, blockHeight uint64) error

	// Close closes the Kafka producer connection
	Close() error
}

// NoOpKafkaProducer is a placeholder that does nothing
// Replace with actual Kafka implementation when ready
type NoOpKafkaProducer struct{}

func (n *NoOpKafkaProducer) PublishBlockCommitted(ctx context.Context, height uint64, hash [32]byte, txCount int) error {
	// TODO: Implement Kafka publishing
	return nil
}

func (n *NoOpKafkaProducer) PublishStateTransition(ctx context.Context, version uint64, root [32]byte) error {
	// TODO: Implement Kafka publishing
	return nil
}

func (n *NoOpKafkaProducer) PublishValidationFailure(ctx context.Context, reason string, blockHeight uint64) error {
	// TODO: Implement Kafka publishing
	return nil
}

func (n *NoOpKafkaProducer) Close() error {
	return nil
}

// Integration points for Kafka (optional)
func (s *Service) publishBlockCommitted(ctx context.Context, height uint64, hash [32]byte, txCount int) {
	// TODO: Wire up Kafka producer when available
	// if s.kafka != nil {
	//     _ = s.kafka.PublishBlockCommitted(ctx, height, hash, txCount)
	// }
}

func (s *Service) publishStateTransition(ctx context.Context, version uint64, root [32]byte) {
	// TODO: Wire up Kafka producer when available
	// if s.kafka != nil {
	//     _ = s.kafka.PublishStateTransition(ctx, version, root)
	// }
}
