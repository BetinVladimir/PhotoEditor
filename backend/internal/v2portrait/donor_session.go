package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// LockOpenEyesDonorSession binds the exact canonical confirmation and face
// evidence refs to current identity/session/source locks in the caller transaction.
// Document geometry/bytes, qualification and publication gates remain separate.
func LockOpenEyesDonorSession(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project, shootSession uuid.UUID, params OpenEyesParams, confirmationRaw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || shootSession == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	proof, err := VerifyOpenEyesConfirmation(params, append([]byte(nil), confirmationRaw...))
	if err != nil {
		return err
	}
	if proof.ActorID != who.UserID || proof.DonorObservationID == nil || *proof.DonorObservationID == proof.SourceObservationID {
		return v2input.ErrInvalid
	}
	body, _, err := proof.Compile()
	if err != nil {
		return err
	}
	sourceFaces, err := canonical.Marshal(params.SourceFaces)
	if err != nil {
		return err
	}
	donorFaces, err := canonical.Marshal(params.DonorFaces)
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
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.lock_v28_donor_session($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb)", tenant, project, shootSession, who.UserID, who.SessionID, string(body), string(sourceFaces), string(donorFaces)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
