package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// PrepareDefectManualTaskInputs discovers original protected children for every
// declared manual face mask, including inactive masks. Face/person/policy
// qualification and final retained transaction admission remain caller duties.
func (s SQLTransferSourceReader) PrepareDefectManualTaskInputs(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, faces []review.DefectProposalFaceInput, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask) (review.DefectProposalTaskInputs, error) {
	inputs, _, err := s.prepareDefectManualTaskProof(ctx, who, tenantID, binding, color, faces, diameter, permanent, additional)
	return inputs, err
}

func (s SQLTransferSourceReader) prepareDefectManualTaskProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, faces []review.DefectProposalFaceInput, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask) (review.DefectProposalTaskInputs, []manualPreparationProof, error) {
	s = s.withManualRasterScope()
	empty := review.DefectProposalTaskInputs{}
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	if faces == nil || len(faces) > 64 || len(additional) > 64 {
		return empty, nil, v2input.ErrInvalid
	}
	// Own policy collections before the first dependency IO/callback.
	policy := struct {
		Faces      []review.DefectProposalFaceInput
		Permanent  *review.ProtectionMask
		Additional []review.ProtectionMask
	}{faces, permanent, additional}
	policyBytes, err := canonical.Marshal(policy)
	if err != nil {
		return empty, nil, err
	}
	if len(policyBytes) > 4194304 {
		return empty, nil, v2input.ErrInvalid
	}
	if err = json.Unmarshal(policyBytes, &policy); err != nil {
		return empty, nil, err
	}
	if _, err = s.VerifyQualifiedFaceSourcePixels(ctx, who, tenantID, binding, color); err != nil {
		return empty, nil, err
	}
	sourceBody, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, nil, err
	}
	imageRef := review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}
	// Refuse malformed Params/dependency identities before reading manual objects.
	if _, err = review.BuildDefectProposalTaskInputsFromImage(ctx, imageRef, sourceBody, policy.Faces, diameter, policy.Permanent, policy.Additional, nil); err != nil {
		return empty, nil, err
	}
	manuals := make([]review.OriginalManualProtection, 0, len(policy.Faces))
	proofs := make([]manualPreparationProof, 0, len(policy.Faces))
	var metadata int
	for _, face := range policy.Faces {
		if face.Manual == nil {
			continue
		}
		original, proof, err := s.readManualPreparationProof(ctx, who, tenantID, binding, *face.Manual)
		if err != nil {
			return empty, nil, err
		}
		if len(original.Body) > 4194304-metadata {
			return empty, nil, v2input.ErrInvalid
		}
		metadata += len(original.Body)
		manuals = append(manuals, original)
		proofs = append(proofs, proof)
	}
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(ctx, imageRef, sourceBody, policy.Faces, diameter, policy.Permanent, policy.Additional, manuals)
	if err != nil {
		return empty, nil, err
	}
	// Re-read all manual dependencies after the entire preparation batch.
	for _, original := range manuals {
		current, err := s.ReadManualPreparation(ctx, who, tenantID, binding, original.Target)
		if err != nil {
			return empty, nil, err
		}
		if current.Root != original.Root || !bytes.Equal(current.Body, original.Body) {
			return empty, nil, v2input.ErrConflict
		}
	}
	if _, err = s.VerifyQualifiedFaceSourcePixels(ctx, who, tenantID, binding, color); err != nil {
		return empty, nil, err
	}
	currentSource, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, nil, err
	}
	if !bytes.Equal(currentSource, sourceBody) {
		return empty, nil, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	for _, proof := range proofs {
		if err := s.recheckManualPreparationProof(ctx, who, tenantID, binding, proof); err != nil {
			return empty, nil, err
		}
	}
	return compiled, proofs, nil
}
