package p2p

import (
	"context"

	"backend/pkg/consensus/api"
	"backend/pkg/consensus/messages"
	"backend/pkg/utils"
	"github.com/libp2p/go-libp2p/core/peer"
)

// AttachConsensusHandlers wires P2P topics to the consensus engine.
// topicMap maps topic string -> messages.MessageType (Proposal/Vote/ViewChange/NewView/Heartbeat/Evidence).
func AttachConsensusHandlers(r *Router, eng *api.ConsensusEngine, topicMap map[string]messages.MessageType) error {
	r.log.Info("AttachConsensusHandlers called - wiring P2P to consensus")
	for topic, mt := range topicMap {
		t := topic
		mtype := mt
		r.log.Info("Subscribing to topic", utils.ZapString("topic", t), utils.ZapAny("msgType", mtype))
		if err := r.Subscribe(t, func(ctx context.Context, from peer.ID, data []byte) error {
			// peerID type is from libp2p; we expose as string address for auditing.
			return eng.OnMessageReceived(ctx, from.String(), mtype, data)
		}); err != nil {
			return err
		}
	}
	// Provide outbound publisher to engine
	eng.SetNetwork(&routerPublisher{r: r})
	r.log.Info("SetNetwork called on consensus engine - P2P publisher attached")
	return nil
}

type routerPublisher struct{ r *Router }

func (p *routerPublisher) Publish(ctx context.Context, topic string, data []byte) error {
	p.r.log.Info("P2P publishing consensus message", utils.ZapString("topic", topic), utils.ZapInt("bytes", len(data)))
	return p.r.Publish(topic, data)
}
