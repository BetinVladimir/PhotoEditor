package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type OpenEyesPreparationProof struct {
	Proof  []byte
	Params OpenEyesParams
}

// ReadOpenEyesPreparationProof binds current confirmed observations, consent,
// qualification and face IDs. It is read-only; publication and admission must
// recheck authority under their own locks.
func ReadOpenEyesPreparationProof(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	tenant, project, shoot, snapshot uuid.UUID, provisional OpenEyesParams,
	pin DonorQualificationPin, sourceObservation, donorObservation uuid.UUID) (OpenEyesPreparationProof, error) {
	var zero OpenEyesPreparationProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || tenant == uuid.Nil || project == uuid.Nil || shoot == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || sourceObservation == uuid.Nil || donorObservation == uuid.Nil || sourceObservation == donorObservation {
		return zero, v2input.ErrInvalid
	}
	paramsRaw, _, err := provisional.Compile()
	if err != nil {
		return zero, err
	}
	pinRaw, err := pin.Compile()
	if err != nil {
		return zero, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT photo.read_v28_open_eyes_preparation_proof($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10)`,
		tenant, project, shoot, snapshot, who.UserID, who.SessionID, string(paramsRaw), string(pinRaw), sourceObservation, donorObservation).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var response struct {
		Proof  json.RawMessage `json:"proof"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(normal, &response) != nil || len(response.Proof) < 2 || len(response.Params) < 2 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(response)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	var bound OpenEyesParams
	if json.Unmarshal(response.Params, &bound) != nil {
		return zero, v2input.ErrInvalid
	}
	boundRaw, _, err := bound.Compile()
	if err != nil || !bytes.Equal(boundRaw, response.Params) {
		return zero, v2input.ErrInvalid
	}
	compare := bound
	compare.SourceFaceID = provisional.SourceFaceID
	compare.DonorFaceID = provisional.DonorFaceID
	if compare != provisional {
		return zero, v2input.ErrInvalid
	}
	var proof DonorConfirmation
	if json.Unmarshal(response.Proof, &proof) != nil {
		return zero, v2input.ErrInvalid
	}
	proofRaw, digest, err := proof.Compile()
	if err != nil || !bytes.Equal(proofRaw, response.Proof) || proof.ActorID != who.UserID || proof.SourceObservationID != sourceObservation || proof.DonorObservationID == nil || *proof.DonorObservationID != donorObservation || proof.SourceFaceID != bound.SourceFaceID || proof.DonorFaceID == nil || *proof.DonorFaceID != bound.DonorFaceID {
		return zero, v2input.ErrInvalid
	}
	bound.Confirmation.SHA256 = digest
	if _, err = VerifyOpenEyesConfirmation(bound, proofRaw); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return OpenEyesPreparationProof{Proof: append([]byte(nil), proofRaw...), Params: bound}, nil
}
