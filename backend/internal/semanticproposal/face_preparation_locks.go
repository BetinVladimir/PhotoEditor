package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Locks selected, privately verified face collections and their qualifications.
// The caller must lock source/auth scope and discover the complete required
// inventory independently. This does not commit, queue, or prove global policy
// completeness. Any error requires rollback of the caller's transaction.
func (s SQLTransferSourceReader) lockFacePreparationProofs(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proofs []facePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || len(proofs) < 1 || len(proofs) > 64 {
		return v2input.ErrInvalid
	}
	entries := make([]facePreparationProof, 0, len(proofs))
	seen := map[uuid.UUID]facePreparationProof{}
	for _, proof := range proofs {
		if !validFacePreparationProofScope(s, who, tenant, binding, proof) {
			return v2input.ErrInvalid
		}
		if _, err := marshalFaceEvidencePins(proof); err != nil {
			return err
		}
		if _, _, err := marshalFaceQualificationPins(proof); err != nil {
			return err
		}
		root := proof.objects[0][0].ref.ArtifactID
		if previous, ok := seen[root]; ok {
			if previous != proof {
				return v2input.ErrConflict
			}
			continue
		}
		seen[root] = proof
		entries = append(entries, proof)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].objects[0][0].ref.ArtifactID.String() < entries[j].objects[0][0].ref.ArtifactID.String()
	})
	lockAll := func() error {
		for _, proof := range entries {
			if err := s.lockFaceEvidenceProof(ctx, tx, who, tenant, binding, proof); err != nil {
				return err
			}
			if err := s.lockFaceQualificationProof(ctx, tx, who, tenant, binding, proof); err != nil {
				return err
			}
		}
		return nil
	}
	if err := lockAll(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_inventory($1,$2,$3,$4)", tenant, s.ProjectID, who.UserID, who.SessionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return lockAll()
}
