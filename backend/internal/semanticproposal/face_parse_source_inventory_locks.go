package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Composes selected verified source and face inventory evidence in the caller's TX.
// Every input is owned/validated before SQL. Source artifacts precede all face inventory
// locks and the single inventory barrier; source pins repeat after the barrier.
// Caller still owes complete discovery, auth/version scope and atomic admission.
func (s SQLTransferSourceReader) lockFaceParseSourcePreparationInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, proofs []faceParsePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || len(proofs) < 1 || len(proofs) > 64 || tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || source.tenant != tenant || source.project != s.ProjectID || source.actor != who.UserID || source.session != who.SessionID || source.bucket != s.Bucket || source.binding != binding || source.iccSHA != color.sha || !source.unitRGB {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(color.sha); err != nil {
		return v2input.ErrInvalid
	}
	if _, err := marshalSourceEvidencePins(source); err != nil {
		return err
	}
	owned := append([]faceParsePreparationProof(nil), proofs...)
	if _, _, err := s.marshalFaceParsePreparationInventory(who, tenant, binding, owned); err != nil {
		return err
	}
	if err := s.lockSourcePreparationProof(ctx, tx, who, tenant, binding, source); err != nil {
		return err
	}
	if err := s.lockFaceParsePreparationInventory(ctx, tx, who, tenant, binding, owned); err != nil {
		return err
	}
	return s.lockSourcePreparationProof(ctx, tx, who, tenant, binding, source)
}
