package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Private proof captures only pins that passed full original-content verification.
// Rechecking is preflight only; final scheduler transactions must lock these rows.
type manualPreparationProof struct {
	tenant, project, actor, session uuid.UUID
	binding                         v2input.SessionVersionBinding
	objects                         defectMaskObjects
	children                        defectManualOutputs
	vectors                         *defectEvidenceObject
}

func (s SQLTransferSourceReader) recheckManualPreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof manualPreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validManualPreparationScope(s.ProjectID, who, tenantID, binding, proof) {
		return v2input.ErrInvalid
	}
	// Reject incomplete or aliased retained evidence before catalog IO. The
	// same canonical closure will be consumed by final transaction pin locks.
	if _, err := marshalManualEvidencePins(proof, s.Bucket); err != nil {
		return err
	}
	current, err := s.manualPreparationObjects(ctx, who, tenantID, binding, proof.objects.Pixel.ref)
	if err != nil {
		return err
	}
	if current != proof.objects {
		return v2input.ErrConflict
	}
	refs := [3]v2input.Ref{proof.children[0].ref, proof.children[1].ref, proof.children[2].ref}
	children, err := s.manualPreparationOutputObjects(ctx, who, tenantID, binding, proof.objects.Pixel.ref, refs)
	if err != nil {
		return err
	}
	if children != proof.children {
		return v2input.ErrConflict
	}
	if proof.vectors != nil {
		vectors, err := s.manualPreparationVectorObject(ctx, who, tenantID, binding, proof.objects.Pixel.ref, proof.vectors.ref)
		if err != nil {
			return err
		}
		if vectors != *proof.vectors {
			return v2input.ErrConflict
		}
	}
	return ctx.Err()
}

func validManualPreparationScope(project uuid.UUID, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof manualPreparationProof) bool {
	if tenantID == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || proof.objects.Root.ref.ArtifactID == uuid.Nil {
		return false
	}
	if proof.tenant != tenantID || proof.project != project || proof.actor != who.UserID || proof.session != who.SessionID || proof.binding != binding {
		return false
	}
	if _, err := domain.ParseDigest(binding.Source.SHA256); err != nil {
		return false
	}
	if _, err := domain.ParseDigest(binding.CanvasHash); err != nil {
		return false
	}
	return true
}
