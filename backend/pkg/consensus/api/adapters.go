package api

import (
	"context"
	"sync"

	"backend/pkg/consensus/leader"
	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
)

// engineState implements messages.ConsensusState backed by engine components.
type engineState struct {
	e    *ConsensusEngine
	seen sync.Map // key: string(msgHash[:]) -> struct{}
}

func newEngineState(e *ConsensusEngine) *engineState {
	return &engineState{e: e}
}

func (s *engineState) GetCurrentView() uint64 {
	return s.e.pacemaker.GetCurrentView()
}

func (s *engineState) GetCurrentHeight() uint64 {
	return s.e.pacemaker.GetCurrentHeight()
}

// NOTE: Until PBFT unification, highest/last committed QC are not bridged here.
func (s *engineState) GetLastCommittedQC() *messages.QC { return nil }
func (s *engineState) GetHighestQC() *messages.QC       { return nil }

func (s *engineState) HasSeenMessage(hash [32]byte) bool {
	_, ok := s.seen.Load(string(hash[:]))
	return ok
}

func (s *engineState) MarkMessageSeen(hash [32]byte) {
	s.seen.Store(string(hash[:]), struct{}{})
}

// validatorSetAdapter adapts types.ValidatorSet + leader.Rotation to messages.ValidatorSet.
type validatorSetAdapter struct {
	vs  types.ValidatorSet
	rot *leader.Rotation
}

func newValidatorSetAdapter(vs types.ValidatorSet, rot *leader.Rotation) *validatorSetAdapter {
	return &validatorSetAdapter{vs: vs, rot: rot}
}

func (a *validatorSetAdapter) IsValidator(keyID messages.KeyID) bool {
	return a.vs.IsValidator(types.ValidatorID(keyID))
}

func (a *validatorSetAdapter) GetValidatorCount() int { return a.vs.GetValidatorCount() }

func (a *validatorSetAdapter) GetValidator(keyID messages.KeyID) (*messages.ValidatorInfo, error) {
	info, err := a.vs.GetValidator(types.ValidatorID(keyID))
	if err != nil || info == nil {
		return nil, err
	}
	return &messages.ValidatorInfo{
		KeyID:      messages.KeyID(info.ID),
		PublicKey:  info.PublicKey,
		Reputation: info.Reputation,
		IsActive:   info.IsActive,
		JoinedAt:   info.JoinedView,
	}, nil
}

func (a *validatorSetAdapter) IsActiveInView(keyID messages.KeyID, view uint64) bool {
	return a.vs.IsActiveInView(types.ValidatorID(keyID), view)
}

func (a *validatorSetAdapter) GetLeaderForView(view uint64) (messages.KeyID, error) {
	id, err := a.rot.GetLeaderForView(context.Background(), view)
	return messages.KeyID(id), err
}
