package api

import (
	"errors"
	"strings"
)

var (
	// ErrStaleProposalView indicates proposal processing used an outdated view.
	ErrStaleProposalView = errors.New("consensus stale proposal view")
	// ErrAlreadyVotedView indicates the node already voted in the proposal view.
	ErrAlreadyVotedView = errors.New("consensus already voted in view")
	// ErrLeaderLost indicates local leadership was lost during submit flow.
	ErrLeaderLost = errors.New("consensus leader lost")
)

func IsStaleProposalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrStaleProposalView) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "proposal view") && strings.Contains(msg, "less than current view")
}

func IsAlreadyVotedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAlreadyVotedView) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "already voted in view")
}

func IsLeaderLostError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrLeaderLost) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not the leader for view") || strings.Contains(msg, "leader ineligible")
}

