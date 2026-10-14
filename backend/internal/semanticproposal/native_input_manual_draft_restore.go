package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Restores and reconstructs the exact original draft without publication or
// admission, then repeats saved config/Params catalog observations before return.
func (s SQLTransferSourceReader) restoreNativeInputManualDraft(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile) (nativeInputManualDraftPlan, error) {
	var zero nativeInputManualDraftPlan
	stage, err := s.restoreNativeInputQualifiedManualStage(ctx, scheduler, who, producers, color)
	if err != nil {
		return zero, err
	}
	plan, err := s.compileNativeInputManualDraft(ctx, who, stage)
	if err != nil {
		return zero, err
	}
	d := producers.detector
	object, err := s.nativeInputDefectSavedParamsObject(ctx, who, d.request, stage.saved.params)
	if err != nil {
		return zero, err
	}
	saved, err := s.readNativeInputSavedStage(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef, "defects.propose")
	if err != nil {
		return zero, err
	}
	if object != stage.params.object || !reflect.DeepEqual(saved, stage.saved) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, d.request.TenantID, d.policy); err != nil {
		return zero, err
	}
	if err := verifyNativeInputManualDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	return plan, nil
}
