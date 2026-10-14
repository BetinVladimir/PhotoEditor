package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Decodes the selected manual closure before any SQL callback. Every original
// manual root and target must match both verified inventory and declared files.
func (s SQLTransferSourceReader) decodeDefectExecutionManualProofs(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectMaskExecutionPreparation) ([]manualPreparationProof, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared.manualCount < 0 || prepared.manualCount > len(prepared.manual) || len(prepared.roots) == 0 || len(prepared.roots) > 4<<20 {
		return nil, v2input.ErrInvalid
	}
	files, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, prepared.face.source)
	if err != nil {
		return nil, err
	}
	var roots []review.ProposalFaceMaskRoot
	if json.Unmarshal([]byte(prepared.roots), &roots) != nil {
		return nil, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(roots)
	if err != nil || string(body) != prepared.roots {
		return nil, v2input.ErrInvalid
	}
	selected := map[v2input.Ref]v2input.Ref{}
	for _, root := range roots {
		if root.Binding.Mask.Selector != "manual" {
			continue
		}
		pixel := v2input.Ref{ArtifactID: root.Binding.Mask.Artifact.ID, SHA256: root.Binding.Mask.Artifact.SHA256}
		ref := v2input.Ref{ArtifactID: root.Root.ID, SHA256: root.Root.SHA256}
		if previous, exists := selected[pixel]; exists && previous != ref {
			return nil, v2input.ErrConflict
		}
		selected[pixel] = ref
	}
	if len(selected) != prepared.manualCount {
		return nil, v2input.ErrInvalid
	}
	proofs := make([]manualPreparationProof, 0, prepared.manualCount)
	seen := map[v2input.Ref]bool{}
	for _, owned := range prepared.manual[:prepared.manualCount] {
		if owned.proof.vectors != nil {
			return nil, v2input.ErrInvalid
		}
		proof := owned.original()
		if !validManualPreparationScope(s.ProjectID, who, tenant, binding, proof) || seen[proof.objects.Pixel.ref] || selected[proof.objects.Pixel.ref] != proof.objects.Root.ref {
			return nil, v2input.ErrInvalid
		}
		if _, err := marshalManualEvidencePins(proof, s.Bucket); err != nil {
			return nil, err
		}
		for _, entry := range []struct {
			pin      defectEvidenceObject
			contract string
		}{{proof.objects.Root, "MaskRef"}, {proof.objects.Pixel, "MaskPixels"}} {
			found := false
			for _, file := range files {
				if file.Ref == entry.pin.ref {
					if file.Contract != entry.contract || file.Object != entry.pin.object {
						return nil, v2input.ErrConflict
					}
					found = true
				}
			}
			if !found {
				return nil, v2input.ErrInvalid
			}
		}
		seen[proof.objects.Pixel.ref] = true
		proofs = append(proofs, proof)
	}
	return proofs, nil
}

// Composes retained manual locks with parser/face/source locks. Caller owns
// rollback and still must lock remaining derived/files/auth before admission.
func (s SQLTransferSourceReader) lockDefectExecutionManualInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectMaskExecutionPreparation) error {
	if tx == nil {
		return v2input.ErrInvalid
	}
	proofs, err := s.decodeDefectExecutionManualProofs(ctx, who, tenant, binding, prepared)
	if err != nil {
		return err
	}
	if err := s.lockDefectExecutionParsersInTransaction(ctx, tx, who, tenant, binding, prepared); err != nil {
		return err
	}
	if len(proofs) > 0 {
		if err := s.lockManualPreparationProofs(ctx, tx, who, tenant, binding, proofs); err != nil {
			return err
		}
	}
	return s.lockDefectExecutionParsersInTransaction(ctx, tx, who, tenant, binding, prepared)
}
