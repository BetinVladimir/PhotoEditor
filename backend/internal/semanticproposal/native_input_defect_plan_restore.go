package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Reconstructs the original CPU defect graph from saved Params, original native
// producers and the saved qualification locator. It never recreates a graph or
// changes protected/manual inputs; changed or unavailable originals refuse.
func (s SQLTransferSourceReader) restoreNativeInputDefectDraftPlan(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask, manuals []review.OriginalManualProtection, pins []nativeDefectInputPin) (nativeDefectDraftPlan, error) {
	var zero nativeDefectDraftPlan
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	d := producers.detector
	saved, err := s.readNativeInputSavedStage(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef, "defects.propose")
	if err != nil {
		return zero, err
	}
	m := saved.recovery
	if m.Report == nil {
		return zero, v2input.ErrInvalid
	}
	paramsObject, err := s.nativeInputDefectSavedParamsObject(ctx, who, d.request, saved.params)
	if err != nil {
		return zero, err
	}
	dependencies, err := s.restoreNativeInputDefectDependencies(ctx, scheduler, who, producers, color, diameter, permanent, additional, manuals)
	if err != nil {
		return zero, err
	}
	if saved.person != nil {
		if dependencies.preparation.person == nil {
			return zero, v2input.ErrConflict
		}
		if err := matchNativeDefectPersonRecovery(ctx, *saved.person, *dependencies.preparation.person); err != nil {
			return zero, err
		}
		expected, err := nativeDefectPersonInputPins(ctx, dependencies.preparation)
		if err != nil {
			return zero, err
		}
		if len(pins) != 0 && !reflect.DeepEqual(pins, expected) {
			return zero, v2input.ErrConflict
		}
		pins = expected
	} else if dependencies.preparation.person != nil {
		return zero, v2input.ErrConflict
	}
	qualification, err := s.prepareNativeDefectStageQualification(ctx, who, dependencies.preparation, color, *m.Report, m.Costs)
	if err != nil {
		return zero, err
	}
	prepared, err := s.compileNativeDefectTask(ctx, who, dependencies, qualification, defectEvidenceObject{ref: saved.params, object: paramsObject}, pins, m.RAM, m.VRAM, m.Scratch, m.Output)
	if err != nil {
		return zero, err
	}
	prepared.legacyQueueOmitted, err = nativePreparationLegacyQueue(saved.config)
	if err != nil { return zero, err }
	plan, err := s.compileNativeDefectDraftPlan(ctx, who, prepared, m.Reserved)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(saved, plan.shell); err != nil {
		return zero, err
	}
	afterObject, err := s.nativeInputDefectSavedParamsObject(ctx, who, d.request, saved.params)
	if err != nil {
		return zero, err
	}
	after, err := s.readNativeInputSavedStage(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef, "defects.propose")
	if err != nil {
		return zero, err
	}
	if paramsObject != afterObject || !reflect.DeepEqual(saved, after) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, d.request.TenantID, d.policy); err != nil {
		return zero, err
	}
	return plan, nil
}
