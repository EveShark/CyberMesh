package api

import (
	"context"
	"fmt"

	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
)

// validatorAdapter adapts messages.Validator to types.MessageValidator.
type validatorAdapter struct{ v *messages.Validator }

func newValidatorAdapter(v *messages.Validator) types.MessageValidator {
	return &validatorAdapter{v: v}
}

func (a *validatorAdapter) ValidateProposal(ctx context.Context, p interface{}) error {
	prop, ok := p.(*messages.Proposal)
	if !ok {
		return fmt.Errorf("invalid proposal type")
	}
	return a.v.ValidateProposal(ctx, prop)
}

func (a *validatorAdapter) ValidateVote(ctx context.Context, v interface{}) error {
	vote, ok := v.(*messages.Vote)
	if !ok {
		return fmt.Errorf("invalid vote type")
	}
	return a.v.ValidateVote(ctx, vote)
}

func (a *validatorAdapter) ValidateQC(ctx context.Context, qc types.QC) error {
	mqc := &messages.QC{
		View:       qc.GetView(),
		Height:     qc.GetHeight(),
		Round:      0,
		BlockHash:  qc.GetBlockHash(),
		Signatures: qc.GetSignatures(),
		Timestamp:  qc.GetTimestamp(),
	}
	return a.v.ValidateQC(ctx, mqc)
}

func (a *validatorAdapter) ValidateViewChange(ctx context.Context, vc interface{}) error {
	x, ok := vc.(*messages.ViewChange)
	if !ok {
		return fmt.Errorf("invalid viewchange type")
	}
	return a.v.ValidateViewChange(ctx, x)
}

func (a *validatorAdapter) ValidateNewView(ctx context.Context, nv interface{}) error {
	x, ok := nv.(*messages.NewView)
	if !ok {
		return fmt.Errorf("invalid newview type")
	}
	return a.v.ValidateNewView(ctx, x)
}

func (a *validatorAdapter) ValidateHeartbeat(ctx context.Context, hb interface{}) error {
	x, ok := hb.(*messages.Heartbeat)
	if !ok {
		return fmt.Errorf("invalid heartbeat type")
	}
	return a.v.ValidateHeartbeat(ctx, x)
}

func (a *validatorAdapter) ValidateEvidence(ctx context.Context, ev interface{}) error {
	x, ok := ev.(*messages.Evidence)
	if !ok {
		return fmt.Errorf("invalid evidence type")
	}
	return a.v.ValidateEvidence(ctx, x)
}
