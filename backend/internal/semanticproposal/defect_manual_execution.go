package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Retains external vector metadata by value, with no mutable pointer alias.
type defectManualExecutionProof struct {
	proof     manualPreparationProof
	vector    defectEvidenceObject
	hasVector bool
}

func ownDefectManualExecutionProof(proof manualPreparationProof) defectManualExecutionProof {
	owned := defectManualExecutionProof{proof: proof}
	if proof.vectors != nil {
		owned.vector = *proof.vectors
		owned.hasVector = true
		owned.proof.vectors = nil
	}
	return owned
}
func (p defectManualExecutionProof) original() manualPreparationProof {
	proof := p.proof
	if p.hasVector {
		vector := p.vector
		proof.vectors = &vector
	}
	return proof
}

// Uses verified root inventory, then reads original full manual closure. This is
// preflight evidence only; final admission must lock retained objects and scope.
func (s SQLTransferSourceReader) readDefectManualExecutionProofs(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, roots []review.ProposalFaceMaskRoot) ([]defectManualExecutionProof, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(roots) > 128 {
		return nil, v2input.ErrInvalid
	}
	retained := make([]defectManualExecutionProof, 0)
	seen := map[v2input.Ref]defectManualExecutionProof{}
	for _, root := range roots {
		if root.Binding.Mask.Selector != "manual" {
			continue
		}
		_, proof, err := s.readManualPreparationProof(ctx, who, tenant, binding, root.Binding.Mask)
		if err != nil {
			return nil, err
		}
		if proof.objects.Root.ref.ArtifactID != root.Root.ID || proof.objects.Root.ref.SHA256 != root.Root.SHA256 {
			return nil, v2input.ErrConflict
		}
		owned := ownDefectManualExecutionProof(proof)
		if previous, exists := seen[proof.objects.Pixel.ref]; exists {
			if previous != owned {
				return nil, v2input.ErrConflict
			}
			continue
		}
		if len(retained) >= 64 {
			return nil, v2input.ErrInvalid
		}
		seen[proof.objects.Pixel.ref] = owned
		retained = append(retained, owned)
	}
	for _, proof := range retained {
		if err := s.recheckManualPreparationProof(ctx, who, tenant, binding, proof.original()); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return retained, nil
}
