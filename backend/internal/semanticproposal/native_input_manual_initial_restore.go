package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Restores the exact original manual plan for downstream result verification.
// All accepted Params are independently reread; limits/report/costs come only
// from saved metadata. No draft, publication, registration or receipt is created.
func (s SQLTransferSourceReader) restoreNativeInputManualInitialPlan(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile) (nativeInputManualInitialDraftPlan, error) {
	var zero nativeInputManualInitialDraftPlan
	original, err := s.restoreNativeInputManualDraft(ctx, scheduler, who, producers, color)
	if err != nil {
		return zero, err
	}
	stage := original.prepared.stage
	params, err := s.readNativeInputPublishedManualParams(ctx, who, producers, nativeInputPublishedManualParams{selection: stage.dependencies.selection}, color)
	if err != nil {
		return zero, err
	}
	if params.params != stage.params || !reflect.DeepEqual(params.published.selection, stage.dependencies.selection) {
		return zero, v2input.ErrConflict
	}
	deps := nativeInputManualInitialDependencies{dependencies: stage.dependencies, params: params}
	r := stage.saved.recovery
	if r.Report == nil {
		return zero, v2input.ErrInvalid
	}
	qualified, err := s.prepareNativeInputManualInitialQualification(ctx, who, deps, color, *r.Report, r.Costs)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(qualified.qualification, stage.qualification) {
		return zero, v2input.ErrConflict
	}
	task, err := s.compileNativeInputManualInitialTask(ctx, who, qualified, r.RAM, r.VRAM, r.Scratch, r.Output)
	if err != nil {
		return zero, err
	}
	task.legacyQueueOmitted, err = nativePreparationLegacyQueue(stage.saved.config)
	if err != nil { return zero, err }
	plan, err := s.compileNativeInputManualInitialDraft(ctx, who, task, r.Reserved)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(stage.saved, plan.shell); err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(plan.shell, original.shell) {
		return zero, v2input.ErrConflict
	}
	after, err := s.restoreNativeInputManualDraft(ctx, scheduler, who, producers, color)
	if err != nil {
		return zero, err
	}
	if after.seal != original.seal || !reflect.DeepEqual(after.shell, original.shell) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}
