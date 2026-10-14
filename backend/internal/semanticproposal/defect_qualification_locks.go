package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Caller owns the transaction and must roll back on any failure.
func (s SQLTransferSourceReader) lockDefectQualificationPreparation(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, proof defectQualificationPreparationProof, expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	pin, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, proof, expected)
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
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_defect_qualification($1,$2,$3,$4,$5,$6)", tenant, s.ProjectID, who.UserID, who.SessionID, proof.expected, pin).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) marshalDefectQualificationPreparation(ctx context.Context, who auth.Identity, tenant uuid.UUID, proof defectQualificationPreparationProof, expected v2qualification.Expected) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || proof.tenant != tenant || proof.project != s.ProjectID || proof.actor != who.UserID || proof.session != who.SessionID || proof.bucket != s.Bucket || len(proof.expected) == 0 || len(proof.expected) > 4<<20 {
		return nil, v2input.ErrInvalid
	}
	var retained v2qualification.Expected
	if json.Unmarshal([]byte(proof.expected), &retained) != nil || !sameQualificationPolicyExpected(retained, expected) {
		return nil, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(retained)
	if err != nil || string(body) != proof.expected {
		return nil, v2input.ErrInvalid
	}
	q := proof.report
	if retained.Operation != "defects.propose" || retained.Version != "1" || q.contract != "QualificationReport" || q.backend != retained.Backend || q.ref.ArtifactID == uuid.Nil || q.object.SHA256 != q.ref.SHA256 || q.object.Key == "" || q.object.VersionID == "" || q.object.VersionID == "null" || q.object.Bytes < 2 || q.object.Bytes > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(q.ref.SHA256); err != nil {
		return nil, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(personEvidencePin{q.contract, q.ref.ArtifactID, q.ref.SHA256, proof.bucket, q.object.Key, q.object.VersionID, q.object.Bytes, q.backend})
	if err != nil || len(pin) > 16384 {
		return nil, v2input.ErrInvalid
	}
	return pin, nil
}
