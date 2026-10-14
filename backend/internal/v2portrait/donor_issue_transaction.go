package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type DonorConfirmationTransactions interface {
	Begin(context.Context) (pgx.Tx, error)
}

// PreparedDonorConfirmation is committed issuance evidence, not an authorization
// token. Reservation/completion must recheck live consent and issuer provenance.
type PreparedDonorConfirmation struct {
	Body   []byte
	SHA256 string
	Params OpenEyesParams
}

// PrepareOpenEyesConfirmation completes all relational locks before returning
// bytes to an object publisher. No database transaction spans object IO.
func PrepareOpenEyesConfirmation(ctx context.Context, db DonorConfirmationTransactions, who auth.Identity,
	tenant, project, shootSession uuid.UUID, params OpenEyesParams, sourceObservation, donorObservation uuid.UUID) (PreparedDonorConfirmation, error) {
	var zero PreparedDonorConfirmation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || tenant == uuid.Nil || project == uuid.Nil || shootSession == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || sourceObservation == uuid.Nil || donorObservation == uuid.Nil || sourceObservation == donorObservation {
		return zero, v2input.ErrInvalid
	}
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	if _, err := DonorDirectInputs(params); err != nil {
		return zero, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	body, digest, err := BuildOpenEyesConfirmation(ctx, tx, who, tenant, project, shootSession, params, sourceObservation, donorObservation)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	bound := params
	bound.Confirmation.SHA256 = digest
	return PreparedDonorConfirmation{Body: body, SHA256: digest, Params: bound}, nil
}
