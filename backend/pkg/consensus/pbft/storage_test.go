//go:build cockroach_persistence

package pbft

import (
	"context"
	"testing"

	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
)

type mockStorageBackend struct {
	proposals map[string][]byte
}

func newMockStorageBackend() *mockStorageBackend {
	return &mockStorageBackend{proposals: make(map[string][]byte)}
}

func (m *mockStorageBackend) SaveProposal(ctx context.Context, hash []byte, height uint64, view uint64, proposer []byte, data []byte) error {
	m.proposals[string(hash)] = data
	return nil
}

// Implement other interface methods with no-ops for test brevity
func (m *mockStorageBackend) LoadProposal(ctx context.Context, hash []byte) ([]byte, error) {
	return nil, nil
}
func (m *mockStorageBackend) ListProposals(ctx context.Context, minHeight uint64, limit int) ([]types.ProposalRecord, error) {
	return nil, nil
}
func (m *mockStorageBackend) SaveQC(ctx context.Context, hash []byte, height uint64, view uint64, data []byte) error {
	return nil
}
func (m *mockStorageBackend) LoadQC(ctx context.Context, hash []byte) ([]byte, error) {
	return nil, nil
}
func (m *mockStorageBackend) ListQCs(ctx context.Context, minHeight uint64, limit int) ([]types.QCRecord, error) {
	return nil, nil
}
func (m *mockStorageBackend) SaveVote(ctx context.Context, view uint64, height uint64, voter []byte, blockHash []byte, voteHash []byte, data []byte) error {
	return nil
}
func (m *mockStorageBackend) ListVotes(ctx context.Context, minHeight uint64, limit int) ([]types.VoteRecord, error) {
	return nil, nil
}
func (m *mockStorageBackend) SaveCommittedBlock(ctx context.Context, height uint64, hash []byte, qc []byte) error {
	return nil
}
func (m *mockStorageBackend) LoadLastCommitted(ctx context.Context) (uint64, []byte, []byte, error) {
	return 0, nil, nil, nil
}
func (m *mockStorageBackend) SaveEvidence(ctx context.Context, hash []byte, height uint64, data []byte) error {
	return nil
}
func (m *mockStorageBackend) LoadEvidence(ctx context.Context, hash []byte) ([]byte, error) {
	return nil, nil
}
func (m *mockStorageBackend) ListEvidence(ctx context.Context, minHeight uint64, limit int) ([]types.EvidenceRecord, error) {
	return nil, nil
}
func (m *mockStorageBackend) DeleteBefore(ctx context.Context, height uint64) error { return nil }
func (m *mockStorageBackend) Close() error                                          { return nil }

func TestStorageStoresProposal(t *testing.T) {
	backend := newMockStorageBackend()
	storage := NewStorage(backend, nil, nil, nil)

	proposal := &messages.Proposal{}
	proposal.BlockHash = messages.BlockHash{1}
	proposal.Height = 1
	proposal.View = 1
	proposal.ProposerID = messages.ValidatorID{2}

	if err := storage.StoreProposal(proposal); err != nil {
		t.Fatalf("StoreProposal failed: %v", err)
	}

	if len(backend.proposals) == 0 {
		t.Fatalf("proposal not saved to backend")
	}
}
