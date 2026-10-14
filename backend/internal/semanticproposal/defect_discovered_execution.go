package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Discovers face report refs from server policy and the scoped trusted registry.
// All report refs come from the scoped trusted registry.
// Catalog candidates are not authority: complete
// original report/source/producer verification runs before returning evidence.
func (s SQLTransferSourceReader) prepareDiscoveredDefectExecution(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, pins DefectOriginPins, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (defectPolicyExecutionPreparation, error) {
	var zero defectPolicyExecutionPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
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
	defectReport, err := s.discoverDefectQualificationReport(ctx, who, tenant, selected["defects.propose"])
	if err != nil {
		return zero, err
	}
	var refs [3]v2input.Ref
	for i, operation := range nativeFaceReportOperations {
		refs[i], err = s.discoverFaceQualificationReport(ctx, who, tenant, selected[operation])
		if err != nil {
			return zero, err
		}
	}
	reports := nativeDefectQualificationReports{Defect: defectReport, Faces: [2]v2input.Ref{refs[0], refs[1]}, Parser: [2]v2input.Ref{refs[2], refs[1]}}
	prepared, err := s.prepareSelectedDefectExecution(ctx, who, tenant, binding, color, pins, reports, published, nodes, costs, ram, vram, scratch, output)
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
