package api

import (
	"context"

	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
)

type encoderAdapter struct{ e *messages.Encoder }

func newEncoderAdapter(e *messages.Encoder) types.MessageEncoder { return &encoderAdapter{e: e} }

func (a *encoderAdapter) Encode(msg types.Message) ([]byte, error) {
	return a.e.Encode(msg)
}

func (a *encoderAdapter) VerifyAndDecode(ctx context.Context, data []byte, msgType types.MessageType) (types.Message, error) {
	// messages.MessageType is alias of types.MessageType; safe cast
	return a.e.VerifyAndDecode(ctx, data, messages.MessageType(msgType))
}

func (a *encoderAdapter) VerifyQC(ctx context.Context, qc types.QC) error {
	mqc := &messages.QC{
		View:         qc.GetView(),
		Height:       qc.GetHeight(),
		Round:        0,
		BlockHash:    qc.GetBlockHash(),
		Signatures:   qc.GetSignatures(),
		Timestamp:    qc.GetTimestamp(),
		AggregatorID: types.ValidatorID{},
	}
	return a.e.VerifyQC(ctx, mqc)
}

func (a *encoderAdapter) ClearCache() { a.e.ClearCache() }

func (a *encoderAdapter) GetCacheStats() (size, capacity int) { return a.e.GetCacheStats() }
