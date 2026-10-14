package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Locks policy/import/report authority only. Caller must also lock complete
// source scope and producer/content evidence in the same TX and rollback on error.
func (s SQLTransferSourceReader) lockFaceParseQualificationProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof faceParsePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !validFaceParsePreparationProofScope(s, who, tenantID, binding, proof) {
		return v2input.ErrInvalid
	}
	policies, pins, err := marshalSelectedFaceQualificationPins(proof.evidence, [2]string{"face.parse", "face.detect"})
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	for i := range policies {
		if err := ctx.Err(); err != nil {
			return err
		}
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_parse_qualification($1,$2,$3,$4,$5,$6)", tenantID, s.ProjectID, who.UserID, who.SessionID, policies[i], pins[i]).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
	}
	return nil
}
