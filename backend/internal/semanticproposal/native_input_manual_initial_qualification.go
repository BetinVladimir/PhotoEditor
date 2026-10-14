package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualInitialStage struct {
	dependencies  nativeInputManualInitialDependencies
	qualification nativeInputManualStageQualification
}

// Initial CPU policy/report selection, without a saved defect draft. Report and
// costs are server-selected; they must match current independent qualification.
func (s SQLTransferSourceReader) prepareNativeInputManualInitialQualification(ctx context.Context, who auth.Identity, initial nativeInputManualInitialDependencies, color TransferColorProfile, report v2input.Ref, costs v2input.SemanticQualifiedCosts) (nativeInputManualInitialStage, error) {
	var zero nativeInputManualInitialStage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(initial.params.published.selection, initial.dependencies.selection) || initial.params.params.ref != initial.dependencies.selection.paramsRef {
		return zero, v2input.ErrConflict
	}
	qualified, err := s.prepareNativeInputManualQualification(ctx, who, initial.dependencies, report, costs, func() error {
		current, err := s.readNativeInputPublishedManualParams(ctx, who, initial.dependencies.producers, initial.params.published, color)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, initial.params) {
			return v2input.ErrConflict
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	return nativeInputManualInitialStage{dependencies: initial, qualification: qualified}, nil
}
