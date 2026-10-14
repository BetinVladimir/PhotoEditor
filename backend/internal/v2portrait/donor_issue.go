package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// BuildOpenEyesConfirmation derives identity/consent fields from trusted DB
// observations under current session locks. It neither publishes an artifact
// nor proves face geometry. The caller retains the transaction through issuance.
func BuildOpenEyesConfirmation(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project, shootSession uuid.UUID, params OpenEyesParams, sourceObservation, donorObservation uuid.UUID) ([]byte, string, error) {
	invalid := func() ([]byte, string, error) { return nil, "", v2input.ErrInvalid }
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || shootSession == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || sourceObservation == uuid.Nil || donorObservation == uuid.Nil || sourceObservation == donorObservation {
		return invalid()
	}
	body, _, err := params.Compile()
	if err != nil {
		return nil, "", err
	}
	if _, err := DonorDirectInputs(params); err != nil {
		return nil, "", err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return nil, "", err
	}
	if role != "photo_scheduler" {
		return invalid()
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.build_v28_donor_confirmation($1,$2,$3,$4,$5,$6::jsonb,$7,$8)", tenant, project, shootSession, who.UserID, who.SessionID, string(body), sourceObservation, donorObservation).Scan(&raw); err != nil {
		return nil, "", err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return invalid()
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return invalid()
	}
	var proof DonorConfirmation
	if json.Unmarshal(raw, &proof) != nil {
		return invalid()
	}
	if proof.ActorID != who.UserID || proof.SourceObservationID != sourceObservation || proof.DonorObservationID == nil || *proof.DonorObservationID != donorObservation {
		return invalid()
	}
	canonicalBody, digest, err := proof.Compile()
	if err != nil {
		return nil, "", err
	}
	if !bytes.Equal(normalized, canonicalBody) {
		return invalid()
	}
	bound := params
	bound.Confirmation.SHA256 = digest
	if _, err := VerifyOpenEyesConfirmation(bound, canonicalBody); err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return canonicalBody, digest, nil
}
