package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// GlareDonorPreparationProof is a short-lived, read-only proposal input. It
// never substitutes for the issuer's later live consent and source rechecks.
type GlareDonorPreparationProof struct {
	Proof  []byte
	Params GlareDonorIssuerParams
}

func ReadGlareDonorPreparationProof(ctx context.Context,
	db DonorConfirmationTransactions, who auth.Identity,
	tenant, project, shoot, snapshot uuid.UUID, paramsRaw, pinRaw []byte,
	sourceObservation, donorObservation uuid.UUID) (GlareDonorPreparationProof, error) {
	var zero GlareDonorPreparationProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		tenant == uuid.Nil || project == uuid.Nil || shoot == uuid.Nil ||
		snapshot == uuid.Nil || sourceObservation == uuid.Nil ||
		donorObservation == uuid.Nil || sourceObservation == donorObservation {
		return zero, v2input.ErrInvalid
	}
	params, err := parseGlareDonorIssuerParams(paramsRaw)
	if err != nil {
		return zero, err
	}
	pinNormal, err := canonical.Bytes(pinRaw)
	if err != nil || !bytes.Equal(pinNormal, pinRaw) || len(pinRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
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
	if err = tx.QueryRow(ctx,
		"SELECT photo.read_v28_glare_donor_preparation_proof($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10)",
		tenant, project, shoot, snapshot, who.UserID, who.SessionID,
		string(paramsRaw), string(pinRaw), sourceObservation, donorObservation).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	var proof DonorConfirmation
	if json.Unmarshal(raw, &proof) != nil {
		return zero, v2input.ErrInvalid
	}
	canonicalProof, digest, err := proof.Compile()
	if err != nil || proof.ActorID != who.UserID ||
		proof.SourceObservationID != sourceObservation ||
		proof.DonorObservationID == nil || *proof.DonorObservationID != donorObservation ||
		proof.Donor == nil || proof.Source != params.Base || *proof.Donor != params.Donor {
		return zero, v2input.ErrInvalid
	}
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalRaw, canonicalProof) {
		return zero, v2input.ErrInvalid
	}
	params.SourceFaceID, params.DonorFaceID = proof.SourceFaceID, *proof.DonorFaceID
	params.Confirmation.SHA256 = digest
	open, err := params.openEyesParams()
	if err != nil {
		return zero, err
	}
	if _, err = VerifyOpenEyesConfirmation(open, canonicalProof); err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return GlareDonorPreparationProof{Proof: canonicalProof, Params: params}, nil
}
