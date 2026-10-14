package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// LockDonorIdentity retains current person/grant/observation locks in the
// caller's scheduler transaction. Exact face/version/evidence/session binding
// must be verified separately in that transaction before publishing authority.
func LockDonorIdentity(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project uuid.UUID, proof DonorConfirmation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || proof.ActorID != who.UserID || proof.Donor == nil || proof.DonorObservationID == nil || *proof.DonorObservationID == proof.SourceObservationID {
		return v2input.ErrInvalid
	}
	if _, _, err := proof.Compile(); err != nil {
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
	if err := tx.QueryRow(ctx, "SELECT photo.lock_v28_donor_identity($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenant, project, proof.PersonID, proof.ConsentID, proof.Epoch, proof.SourceObservationID, *proof.DonorObservationID, who.UserID, who.SessionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
