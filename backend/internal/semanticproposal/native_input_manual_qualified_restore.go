package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputRestoredManualStage struct {
	dependencies  nativeInputManualDependencies
	qualification nativeInputManualStageQualification
	saved         nativeInputSavedStage
	params        defectEvidenceObject
}

// Restores original qualification locators/costs and Params identity from saved
// config, never from recovery caller defaults. Still not a compiled/admitted task.
func (s SQLTransferSourceReader) restoreNativeInputQualifiedManualStage(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile) (nativeInputRestoredManualStage, error) {
	var zero nativeInputRestoredManualStage
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
	if saved.recovery.Report == nil {
		return zero, v2input.ErrInvalid
	}
	object, err := s.nativeInputDefectSavedParamsObject(ctx, who, d.request, saved.params)
	if err != nil {
		return zero, err
	}
	dependencies, err := s.restoreNativeInputManualDependencies(ctx, scheduler, who, producers, color)
	if err != nil {
		return zero, err
	}
	if dependencies.selection.paramsRef != saved.params {
		return zero, v2input.ErrConflict
	}
	qualified, err := s.prepareNativeInputManualStageQualification(ctx, who, dependencies, color, *saved.recovery.Report, saved.recovery.Costs)
	if err != nil {
		return zero, err
	}
	currentObject, err := s.nativeInputDefectSavedParamsObject(ctx, who, d.request, saved.params)
	if err != nil {
		return zero, err
	}
	current, err := s.readNativeInputSavedStage(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef, "defects.propose")
	if err != nil {
		return zero, err
	}
	if currentObject != object || !reflect.DeepEqual(saved, current) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, d.request.TenantID, d.policy); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeInputRestoredManualStage{dependencies: dependencies, qualification: qualified, saved: saved, params: defectEvidenceObject{ref: saved.params, object: object}}, nil
}
