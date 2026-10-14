package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualInitialSubmission struct {
	plan   nativeInputManualInitialDraftPlan
	ticket v2input.JobTicket
}

// Server-owned initial assembly. Face references are locators only: original
// manual producer evidence is resolved before Params publication. A child ticket
// must not be exposed as the public semantic parent receipt.
func (b nativePreparationBootstrap) bootstrapManualDefect(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, faces []review.DefectProposalFaceInput, diameter [2]float64, report v2input.Ref, budget nativePreparationBudget) (nativeInputManualInitialSubmission, error) {
	var zero nativeInputManualInitialSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if budget.RAM < 1 || budget.VRAM < 0 || budget.Scratch < 1 || budget.Output < 1 || budget.Output > 9007199254740991-8388608 || budget.Reserved < budget.Output+8388608 || budget.Reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	initial, err := b.Reader.prepareNativeInputManualInitialParams(ctx, who, producers, color, faces, diameter)
	if err != nil {
		return zero, err
	}
	published, err := b.Reader.publishNativeInputManualInitialParams(ctx, who, producers, initial, color, b.Documents)
	if err != nil {
		return zero, err
	}
	dependencies, err := b.Reader.prepareNativeInputManualInitialDependencies(ctx, b.Scheduler, who, producers, published, color)
	if err != nil {
		return zero, err
	}
	qualified, err := b.Reader.prepareNativeInputManualInitialQualification(ctx, who, dependencies, color, report, budget.Costs)
	if err != nil {
		return zero, err
	}
	task, err := b.Reader.compileNativeInputManualInitialTask(ctx, who, qualified, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
	if err != nil {
		return zero, err
	}
	plan, err := b.Reader.compileNativeInputManualInitialDraft(ctx, who, task, budget.Reserved)
	if err != nil {
		return zero, err
	}
	ticket, err := b.Reader.startNativeInputManualInitialPreparation(ctx, b.Scheduler, b.Transport, who, plan, color)
	if err != nil {
		return zero, err
	}
	return nativeInputManualInitialSubmission{plan: plan, ticket: ticket}, nil
}
