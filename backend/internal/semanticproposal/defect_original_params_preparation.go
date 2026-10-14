package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Reconstructs Params only from the original privately qualified producer.
// Full protections and original face order survive recompilation. Exact bytes
// and SHA must match: no reduced policy or silent adjustment is accepted.
// This prepares evidence; publication and locked admission remain separate.
func (s SQLTransferSourceReader) prepareOriginalDefectParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, execution defectPolicyExecutionPreparation) (defectManualParamsPreparation, error) {
	var zero defectManualParamsPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	origin := execution.execution.face.originProof
	if !validQualificationPolicySnapshot(s, who, tenant, execution.policy) || !s.validDefectOriginPreparationProof(who, tenant, binding, origin) {
		return zero, v2input.ErrInvalid
	}
	if err := matchDefectExecutionPolicySnapshot(execution.execution, execution.policy); err != nil {
		return zero, err
	}
	prepared, err := s.prepareOriginDefectParams(ctx, who, tenant, binding, color, origin)
	if err != nil {
		return zero, err
	}
	if prepared.source != execution.execution.face.source.source {
		return zero, v2input.ErrConflict
	}
	return prepared, nil
}

// Reconstructs original Params before execution-profile assembly. The private
// origin proof is catalog provenance, not scientific qualification or admission.
// Full source and manual evidence verification and exact canonical bytes remain
// mandatory; policy/face/parser qualification is performed by the later stage.
func (s SQLTransferSourceReader) prepareOriginDefectParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, origin defectOriginPreparationProof) (defectManualParamsPreparation, error) {
	var zero defectManualParamsPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !s.validDefectOriginPreparationProof(who, tenant, binding, origin) {
		return zero, v2input.ErrInvalid
	}
	if err := s.recheckDefectOriginPreparationProof(ctx, who, tenant, binding, origin); err != nil {
		return zero, err
	}
	pin := origin.objects[0]
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
	if err != nil {
		return zero, err
	}
	var input struct {
		Faces       []review.DefectProposalFaceInput `json:"faces"`
		Diameter    [2]float64                       `json:"diameter_face_fraction"`
		Permanent   *review.ProtectionMask           `json:"permanent"`
		Protections []review.ProtectionMask          `json:"protections"`
	}
	if json.Unmarshal(body, &input) != nil {
		return zero, v2input.ErrInvalid
	}
	prepared, err := s.prepareManualDefectParams(ctx, who, tenant, binding, color, input.Faces, input.Diameter, input.Permanent, input.Protections)
	if err != nil {
		return zero, err
	}
	if prepared.params.params != string(body) || prepared.params.digest != pin.ref.SHA256 {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckDefectOriginPreparationProof(ctx, who, tenant, binding, origin); err != nil {
		return zero, err
	}
	return prepared, nil
}
