package api

import (
	"backend/pkg/consensus/leader"
	"backend/pkg/consensus/messages"
	"context"
	"fmt"
)

type pacemakerPublisherAdapter struct{ e *ConsensusEngine }

func newPacemakerPublisherAdapter(e *ConsensusEngine) *pacemakerPublisherAdapter {
	return &pacemakerPublisherAdapter{e: e}
}

func (a *pacemakerPublisherAdapter) PublishViewChange(ctx context.Context, vc *leader.ViewChangeMsg) error {
	if a.e == nil || a.e.net == nil || a.e.encoder == nil || vc == nil {
		return nil
	}
	m := &messages.ViewChange{
		OldView:   vc.OldView,
		NewView:   vc.NewView,
		Height:    0,
		HighestQC: nil,
		SenderID:  vc.SenderID,
		Timestamp: vc.Timestamp,
		Signature: messages.Signature{Bytes: vc.Signature, KeyID: vc.SenderID, Timestamp: vc.Timestamp},
	}
	data, err := a.e.encoder.Encode(m)
	if err != nil {
		return fmt.Errorf("encode vc: %w", err)
	}
	return a.e.net.Publish(ctx, a.e.topicFor(messages.TypeViewChange), data)
}

func (a *pacemakerPublisherAdapter) PublishNewView(ctx context.Context, nv *leader.NewViewMsg) error {
	if a.e == nil || a.e.net == nil || a.e.encoder == nil || nv == nil {
		return nil
	}
	vcl := make([]messages.ViewChange, 0, len(nv.ViewChanges))
	for _, lvc := range nv.ViewChanges {
		vcl = append(vcl, messages.ViewChange{
			OldView:   lvc.OldView,
			NewView:   lvc.NewView,
			Height:    0,
			HighestQC: nil,
			SenderID:  lvc.SenderID,
			Timestamp: lvc.Timestamp,
			Signature: messages.Signature{Bytes: lvc.Signature, KeyID: lvc.SenderID, Timestamp: lvc.Timestamp},
		})
	}
	m := &messages.NewView{
		View:        nv.View,
		Height:      0,
		ViewChanges: vcl,
		HighestQC:   nil,
		LeaderID:    nv.LeaderID,
		Timestamp:   nv.Timestamp,
		Signature:   messages.Signature{Bytes: nv.Signature, KeyID: nv.LeaderID, Timestamp: nv.Timestamp},
	}
	data, err := a.e.encoder.Encode(m)
	if err != nil {
		return fmt.Errorf("encode newview: %w", err)
	}
	return a.e.net.Publish(ctx, a.e.topicFor(messages.TypeNewView), data)
}
