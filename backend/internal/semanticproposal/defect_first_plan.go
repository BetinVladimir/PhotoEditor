package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Rebuilds the original producer plan using privately selected qualified
// execution authority. Params publication is deterministic for the parent
// command; all document IO precedes draft/activation transactions.
// Qualification selection and admission remain separate server boundaries.
func (s SQLTransferSourceReader) prepareFirstDefectPlan(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, execution defectPolicyExecutionPreparation, color TransferColorProfile, publisher v2draft.Publisher, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output, reserved int64) (defectPreparationDraftPlan, error) {
	var zero defectPreparationDraftPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || publisher.DB == nil || publisher.Objects == nil || publisher.Bucket != s.Bucket || s.Bucket == "" || len(snapshotBody) < 2 || len(snapshotBody) > 8388608 {
		return zero, v2input.ErrInvalid
	}
	snapshotBody = append([]byte(nil), snapshotBody...)
	published.Inventory.Targets = append([]v2input.SessionVersionBinding(nil), published.Inventory.Targets...)
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if who.SessionID == uuid.Nil || request.ActorID != who.UserID || owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA {
		return zero, v2input.ErrInvalid
	}
	intentSHA, err := canonical.Hash(owned.Intent)
	if err != nil {
		return zero, err
	}
	if published.Ref.ArtifactID == uuid.Nil || published.Ref.SHA256 != intentSHA {
		return zero, v2input.ErrInvalid
	}
	if err := validateTransferMeasuredCosts(costs, execution.execution.face.source.qualification.metrics); err != nil {
		return zero, err
	}
	// Verify parent inventory/snapshot identity before publishing Params.
	targets, err := BindTargets(owned, snapshot, snapshotBody)
	if err != nil {
		return zero, err
	}
	if published.Inventory.Scope.ProjectID != s.ProjectID || len(targets) != len(published.Inventory.Targets) || len(targets) != published.Inventory.Scope.ItemCount {
		return zero, v2input.ErrInvalid
	}
	for i, target := range targets {
		if target != published.Inventory.Targets[i] {
			return zero, v2input.ErrConflict
		}
	}
	prepared, err := s.prepareOriginalDefectParams(ctx, who, owned.TenantID, owned.Intent.Reference, color, execution)
	if err != nil {
		return zero, err
	}
	document := uuid.NewSHA1(owned.RunID, []byte("semantic.defect.params:reference"))
	params, err := s.publishManualDefectVersionParams(ctx, who, owned.TenantID, owned.Intent.Reference, color, publisher, document, prepared)
	if err != nil {
		return zero, err
	}
	proof, err := s.readDefectVersionParamsProof(ctx, who, owned.TenantID, owned.Intent.Reference, color, document, params, prepared)
	if err != nil {
		return zero, err
	}
	return s.prepareDefectPreparationDraft(ctx, who, owned, key, published, snapshot, snapshotBody, execution, proof, color, costs, ram, vram, scratch, output, reserved)
}
