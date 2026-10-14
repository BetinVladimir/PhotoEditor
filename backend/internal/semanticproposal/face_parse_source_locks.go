package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Composes selected verified source and parser evidence in the caller's TX.
// Every input is owned/validated before SQL. Source artifacts precede all parser
// locks and the single inventory barrier; source pins repeat after the barrier.
// Caller still owes complete discovery, auth/version scope and atomic admission.
func (s SQLTransferSourceReader) lockFaceParseSourcePreparationProofs(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, proofs []faceParsePreparationProof) error {
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
	seen := map[uuid.UUID]faceParsePreparationProof{}
	for _, proof := range owned {
		if !validFaceParsePreparationProofScope(s, who, tenant, binding, proof) {
			return v2input.ErrInvalid
		}
		if _, err := marshalFaceParseEvidencePins(proof); err != nil {
			return err
		}
		if _, _, err := marshalSelectedFaceQualificationPins(proof.evidence, [2]string{"face.parse", "face.detect"}); err != nil {
			return err
		}
		root := proof.evidence.objects[0][0].ref.ArtifactID
		if old, ok := seen[root]; ok && old != proof {
			return v2input.ErrConflict
		}
		seen[root] = proof
	}
	if err := s.lockSourcePreparationProof(ctx, tx, who, tenant, binding, source); err != nil {
		return err
	}
	if err := s.lockFaceParsePreparationProofs(ctx, tx, who, tenant, binding, owned); err != nil {
		return err
	}
	return s.lockSourcePreparationProof(ctx, tx, who, tenant, binding, source)
}
