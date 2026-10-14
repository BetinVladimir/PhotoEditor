package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Caller must roll back on failure; success retains selected qualification and
// artifact locks until caller ends the transaction. No run is admitted here.
func (s SQLTransferSourceReader) lockTransferQualificationPreparation(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, proof transferQualificationPreparationProof, expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	pin, body, err := s.marshalTransferQualificationPreparation(ctx, who, tenant, proof, expected)
	if err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ok bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_transfer_qualification_evidence($1,$2,$3,$4,$5,$6)", tenant, s.ProjectID, who.UserID, who.SessionID, pin, body).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) marshalTransferQualificationPreparation(ctx context.Context, who auth.Identity, tenant uuid.UUID, proof transferQualificationPreparationProof, expected v2qualification.Expected) ([]byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || proof.tenant != tenant || proof.project != s.ProjectID || proof.actor != who.UserID || proof.session != who.SessionID || proof.bucket != s.Bucket || len(proof.expected) == 0 || len(proof.expected) > 4<<20 {
		return nil, nil, v2input.ErrInvalid
	}
	var retained v2qualification.Expected
	if json.Unmarshal([]byte(proof.expected), &retained) != nil || !sameQualificationPolicyExpected(retained, expected) {
		return nil, nil, v2qualification.ErrUnqualified
	}
	if err := verifyQualificationConfiguration(proof.pin, expected); err != nil {
		return nil, nil, err
	}
	type objectPin struct {
		Contract string    `json:"contract"`
		ID       uuid.UUID `json:"artifact_id"`
		SHA      string    `json:"sha256"`
		Bucket   string    `json:"bucket"`
		Key      string    `json:"object_key"`
		Version  string    `json:"object_version"`
		Bytes    int64     `json:"bytes"`
	}
	pins := make([]objectPin, 2)
	for i, item := range proof.objects {
		contract, ref, limit := "Calibration", proof.pin.Calibration, int64(1<<20)
		if i == 1 {
			contract, ref, limit = "Report", proof.pin.Report, 8<<20
		}
		if item.contract != contract || item.ref != ref || item.object.SHA256 != ref.SHA256 || item.object.Key == "" || item.object.VersionID == "" || item.object.VersionID == "null" || item.object.Bytes < 2 || item.object.Bytes > limit {
			return nil, nil, v2input.ErrInvalid
		}
		pins[i] = objectPin{contract, ref.ArtifactID, ref.SHA256, s.Bucket, item.object.Key, item.object.VersionID, item.object.Bytes}
	}
	body, err := canonical.Marshal(pins)
	if err != nil || len(body) > 32768 {
		return nil, nil, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(proof.pin)
	if err != nil {
		return nil, nil, err
	}
	return pin, body, nil
}
