package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Private exact content to publish with new-native authority; not a registered
// protection root and not interchangeable with imported-origin parent proof.
type nativeInputManualInitialProtectionPreparation struct {
	document uuid.UUID
	body     string
	pixels   v2input.Ref
	result   completedNativeInputDefect
	plan     nativeInputManualInitialDraftPlan
}

func (s SQLTransferSourceReader) prepareNativeInputManualInitialProtection(ctx context.Context, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile) (nativeInputManualInitialProtectionPreparation, error) {
	var zero nativeInputManualInitialProtectionPreparation
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	result, err := s.readCompletedNativeInputManualInitialDefect(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	parent := plan.prepared.stage.dependencies.dependencies.producers.detector.request
	image := []byte(plan.prepared.stage.dependencies.dependencies.selection.sourceBody)
	e := result.evidence
	body, pixels, err := v2input.BuildSemanticDefectProtectionDocument(ctx, result.catalog.Params, e.Params, result.catalog.Outputs[4], e.Report, result.catalog.Outputs[3], e.Proposal, parent.Intent.Reference, image)
	if err != nil {
		return zero, err
	}
	if pixels != result.catalog.Outputs[1] || pixels != e.Protection {
		return zero, v2input.ErrConflict
	}
	after, err := s.readCompletedNativeInputManualInitialDefect(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(after, result) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	document := uuid.NewSHA1(parent.RunID, []byte("semantic.defect.protection:"+plan.shell.Tasks[0].ID.String()))
	return nativeInputManualInitialProtectionPreparation{document: document, body: string(body), pixels: pixels, result: result, plan: plan}, nil
}
