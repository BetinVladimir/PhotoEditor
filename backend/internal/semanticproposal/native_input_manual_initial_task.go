package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualInitialTaskPreparation struct {
	legacyQueueOmitted bool
	stage    nativeInputManualInitialStage
	compiled nativeInputManualCompiledTask
}

// Uses actual server-selected capacity limits and independent qualification;
// no saved recovery metadata is fabricated for a newly prepared task.
func (s SQLTransferSourceReader) compileNativeInputManualInitialTask(ctx context.Context, who auth.Identity, stage nativeInputManualInitialStage, ram, vram, scratch, output int64) (nativeInputManualInitialTaskPreparation, error) {
	var zero nativeInputManualInitialTaskPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	initial := stage.dependencies
	if !reflect.DeepEqual(initial.params.published.selection, initial.dependencies.selection) {
		return zero, v2input.ErrConflict
	}
	compiled, err := s.compileNativeInputManualTask(ctx, who, initial.dependencies, stage.qualification, initial.params.params, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	return nativeInputManualInitialTaskPreparation{stage: stage, compiled: compiled}, nil
}
