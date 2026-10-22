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

func (s SQLTransferSourceReader) marshalDetectorQualificationProof(ctx context.Context, who auth.Identity, tenant uuid.UUID, proof detectorQualificationProof, expected v2qualification.Expected) ([]byte, error) {
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
	if retained.Operation != "face.detect" || (retained.Backend != "cuda" && retained.Backend != "mps") || retained.Version != "1" || q.contract != "QualificationReport" || q.backend != retained.Backend || q.ref.ArtifactID == uuid.Nil || q.object.SHA256 != q.ref.SHA256 || q.object.Key == "" || q.object.VersionID == "" || q.object.VersionID == "null" || q.object.Bytes < 2 || q.object.Bytes > 8<<20 {
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

// Rechecks retained report authority; final queue admission must lock the same
// policy/report pins in its database transaction.
func (s SQLTransferSourceReader) recheckDetectorQualificationProof(ctx context.Context, who auth.Identity, tenant uuid.UUID, proof detectorQualificationProof, expected v2qualification.Expected) error {
	expected = ownQualificationExpected(expected)
	if _, err := s.marshalDetectorQualificationProof(ctx, who, tenant, proof, expected); err != nil {
		return err
	}
	current, err := s.faceQualificationObject(ctx, who, tenant, expected, proof.report.ref)
	if err != nil {
		return err
	}
	if current != proof.report {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Caller owns the transaction and must roll back on any failure.
func (s SQLTransferSourceReader) lockDetectorQualificationProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, proof detectorQualificationProof, expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	pin, err := s.marshalDetectorQualificationProof(ctx, who, tenant, proof, expected)
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
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_qualification($1,$2,$3,$4,$5,$6)", tenant, s.ProjectID, who.UserID, who.SessionID, proof.expected, pin).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Prevents exchanging a task/profile/input digest after original report checks.
func verifyDetectorTaskQualificationBinding(frozen v2input.FrozenSemanticDetectorTask, proof detectorQualificationProof) error {
	if _, err := domain.ParseDigest(proof.taskSHA); err != nil {
		return v2input.ErrInvalid
	}
	task, err := frozen.DraftTask()
	if err != nil {
		return err
	}
	sum, err := canonical.Hash(task)
	if err != nil {
		return err
	}
	if sum != proof.taskSHA {
		return v2input.ErrConflict
	}
	return nil
}
