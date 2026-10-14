package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller holds the original parent lock and supplies physically verified person
// authority. Existing children retain original Params, report and resource costs.
// The returned receipt is child admission only, never parent completion.
func (b nativePreparationBootstrap) ensureInitialPersonDefect(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, person nativePersonInputAuthority, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeDefectSubmission, error) {
	var zero nativeDefectSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if documents == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return zero, err
	}
	d := producers.detector
	if err := b.Reader.validateNativePersonInputAuthority(ctx, who, d.request, person); err != nil {
		return zero, err
	}
	presence, err := b.Reader.readNativeStagePresence(ctx, who, d, "defects.propose")
	if err != nil {
		return zero, err
	}
	run, _, err := defectPreparationIDs(d.request.RunID)
	if err != nil {
		return zero, err
	}
	publisher := v2input.ScopedPublisher{Documents: documents, Tenant: d.request.TenantID, Run: run}
	switch presence {
	case nativeStageAbsent:
		config, err := b.Reader.selectInitialDefectConfiguration(ctx, who, d.request.TenantID, ownTransferPreparation(capacity), reserved)
		if err != nil {
			return zero, err
		}
		// Full normative supported diameter range. Saved children always replay their
		// own range; this initial choice does not override an existing calibration.
		prepared, err := b.Reader.prepareNativeDefectPersonFaceParams(ctx, who, producers.landmarks, producers.skin, capacity.Color, [2]float64{.002, .012}, person)
		if err != nil {
			return zero, err
		}
		transports, err := recoverNativeInputFaceProducerTransports(ctx, b.Scheduler, who, producers)
		if err != nil {
			return zero, err
		}
		budget := config.Budget
		return b.Reader.startNativeDefectPreparation(ctx, b.Scheduler, b.Documents, publisher, who, prepared, capacity.Color, config.Report, budget.Costs, transports.landmarks, transports.skin, transports.parser, transports.detector, budget.RAM, budget.VRAM, budget.Scratch, budget.Output, budget.Reserved)
	case nativeStagePresent:
		diameter, err := b.Reader.readNativeInputSavedDefectDiameter(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef)
		if err != nil {
			return zero, err
		}
		plan, err := b.Reader.restoreNativeInputDefectDraftPlan(ctx, b.Scheduler, who, producers, capacity.Color, diameter, nil, nil, nil, nil)
		if err != nil {
			return zero, err
		}
		original := plan.prepared.dependencies.preparation.person
		if original == nil || !reflect.DeepEqual(*original, person) {
			return zero, v2input.ErrConflict
		}
		state, err := b.Reader.readNativeInputSavedStageState(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef, "defects.propose")
		if err != nil {
			return zero, err
		}
		phase, err := nativeInputStagePreparationPhase(state)
		if err != nil {
			return zero, err
		}
		if phase == nativePreparationFailed {
			return zero, v2input.ErrConflict
		}
		if phase == nativePreparationBlocked {
			if _, err := b.Reader.persistNativeDefectPreparationInputs(ctx, b.Scheduler, publisher, who, plan, capacity.Color); err != nil {
				return zero, err
			}
		}
		ticket, err := b.Reader.resumeNativeDefectPreparation(ctx, b.Scheduler, who, plan)
		if err != nil {
			return zero, err
		}
		if ticket.RunID != plan.shell.Run || ticket.WorkflowID != plan.shell.WorkflowID || ticket.State != "queued" {
			return zero, v2input.ErrConflict
		}
		return nativeDefectSubmission{plan: plan, ticket: ticket}, nil
	default:
		return zero, v2input.ErrConflict
	}
}
