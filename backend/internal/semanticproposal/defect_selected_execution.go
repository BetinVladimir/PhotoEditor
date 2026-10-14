package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Report refs are deployment-selected private pins. Expected profiles come
// exclusively from the original complete DBA policy inventory; no caller
// supplied Expected, newest-model choice or backend fallback is accepted.
func (s SQLTransferSourceReader) prepareSelectedDefectExecution(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, pins DefectOriginPins, reports nativeDefectQualificationReports, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (defectPolicyExecutionPreparation, error) {
	var zero defectPolicyExecutionPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if reports.Faces[1] != reports.Parser[1] {
		return zero, v2input.ErrConflict
	}
	owned, err := snapshotDefectDescriptors(published, nodes)
	if err != nil {
		return zero, err
	}
	published, nodes = owned.Published, owned.Nodes
	policy, err := s.readQualificationPolicySnapshot(ctx, who, tenant)
	if err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(policy, []string{"defects.propose", "face.landmarks", "face.detect", "face.parse"})
	if err != nil {
		return zero, err
	}
	faces := [2]v2qualification.Expected{selected["face.landmarks"], selected["face.detect"]}
	parser := [2]v2qualification.Expected{selected["face.parse"], selected["face.detect"]}
	prepared, err := s.preparePolicyQualifiedDefectExecutionEvidence(ctx, who, tenant, binding, color, pins, reports.Faces, reports.Parser, faces, parser, reports.Defect, selected["defects.propose"], published, nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	if prepared.policy != policy {
		return zero, v2input.ErrConflict
	}
	current, err := s.readQualificationPolicySnapshot(ctx, who, tenant)
	if err != nil {
		return zero, err
	}
	if current != policy {
		return zero, v2input.ErrConflict
	}
	return prepared, nil
}
