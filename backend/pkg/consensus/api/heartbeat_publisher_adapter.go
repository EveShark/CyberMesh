package api

import (
	"backend/pkg/consensus/leader"
	"backend/pkg/consensus/messages"
	"context"
	"fmt"
)

type heartbeatPublisherAdapter struct{ e *ConsensusEngine }

func newHeartbeatPublisherAdapter(e *ConsensusEngine) *heartbeatPublisherAdapter {
	return &heartbeatPublisherAdapter{e: e}
}

func (a *heartbeatPublisherAdapter) PublishHeartbeat(ctx context.Context, hb *leader.HeartbeatMsg) error {
	if a.e == nil || a.e.net == nil || a.e.encoder == nil || hb == nil {
		return nil
	}
	m := &messages.Heartbeat{
		View:      hb.View,
		Height:    hb.Height,
		LeaderID:  hb.LeaderID,
		Timestamp: hb.Timestamp,
		Signature: messages.Signature{Bytes: hb.Signature, KeyID: hb.LeaderID, Timestamp: hb.Timestamp},
	}
	data, err := a.e.encoder.Encode(m)
	if err != nil {
		return fmt.Errorf("encode heartbeat: %w", err)
	}
	return a.e.net.Publish(ctx, a.e.topicFor(messages.TypeHeartbeat), data)
}
