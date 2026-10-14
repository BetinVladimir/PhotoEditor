package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeDefectProducerTransports struct {
	Landmarks, Skin, Parser, Detector v2input.Built
}

// Server-owned native producer entry point. This child acceptance ticket must
// not be returned as a parent API receipt; public parent continuation is separate.
func (b nativePreparationBootstrap) bootstrapDefect(ctx context.Context, who auth.Identity, landmarks nativeFaceDraftPlan, skin nativeSkinDraftPlan, color TransferColorProfile, diameter [2]float64, report v2input.Ref, transports nativeDefectProducerTransports, budget nativePreparationBudget) (nativeDefectSubmission, error) {
	var zero nativeDefectSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if budget.RAM < 1 || budget.VRAM < 0 || budget.Scratch < 1 || budget.Output < 1 || budget.Output > 9007199254740991-8388608 || budget.Reserved < budget.Output+8388608 || budget.Reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	prepared, err := b.Reader.prepareNativeDefectFaceParams(ctx, who, landmarks, skin, color, diameter, nil, nil, nil)
	if err != nil {
		return zero, err
	}
	return b.Reader.startNativeDefectPreparation(ctx, b.Scheduler, b.Documents, b.Transport, who, prepared, color, report, budget.Costs, transports.Landmarks, transports.Skin, transports.Parser, transports.Detector, budget.RAM, budget.VRAM, budget.Scratch, budget.Output, budget.Reserved)
}
