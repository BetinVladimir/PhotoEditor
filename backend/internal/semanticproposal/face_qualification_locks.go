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

func marshalFaceQualificationPins(proof facePreparationProof) (policies, pins [2]string, err error) {
	return marshalSelectedFaceQualificationPins(proof, [2]string{"face.landmarks", "face.detect"})
}

func marshalSelectedFaceQualificationPins(proof facePreparationProof, operations [2]string) (policies, pins [2]string, err error) {
	var expected [2]v2qualification.Expected
	if json.Unmarshal([]byte(proof.expected), &expected) != nil {
		return policies, pins, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(expected)
	if err != nil || string(body) != proof.expected {
		return policies, pins, v2input.ErrInvalid
	}
	for i, value := range expected {
		if value.Operation != operations[i] || value.Version != "1" || value.Backend != proof.qualification[i].backend {
			return policies, pins, v2input.ErrInvalid
		}
		policy, err := canonical.Marshal(value)
		if err != nil {
			return policies, pins, err
		}
		policies[i] = string(policy)
		q := proof.qualification[i]
		pin, err := canonical.Marshal(personEvidencePin{q.contract, q.ref.ArtifactID, q.ref.SHA256, proof.bucket, q.object.Key, q.object.VersionID, q.object.Bytes, q.backend})
		if err != nil {
			return policies, pins, err
		}
		pins[i] = string(pin)
	}
	return policies, pins, nil
}

// Locks policy/import/report authority only. Caller must also lock complete
// source scope and producer/content evidence in the same TX and rollback on error.
func (s SQLTransferSourceReader) lockFaceQualificationProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof facePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !validFacePreparationProofScope(s, who, tenantID, binding, proof) {
		return v2input.ErrInvalid
	}
	policies, pins, err := marshalFaceQualificationPins(proof)
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
		err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_qualification($1,$2,$3,$4,$5,$6)", tenantID, s.ProjectID, who.UserID, who.SessionID, policies[i], pins[i]).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
	}
	return nil
}
