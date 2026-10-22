package v2draft

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Completed glare checks cannot reuse a running-worker lease or require a
// decided review. The atomic SQL adoption repeats this gate after these reads.
type glareAdoptionAuthority interface {
	Current(context.Context, RestorationArrayScope) error
}

type sqlGlareAdoptionAuthority struct { DB RestorationAdoptionTransactions }

func (a sqlGlareAdoptionAuthority) Current(ctx context.Context, scope RestorationArrayScope) error {
	if err := ctx.Err(); err != nil { return err }
	if a.DB == nil { return v2input.ErrInvalid }
	tx, err := a.DB.Begin(ctx)
	if err != nil { return err }
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", scope.Tenant.String()); err != nil { return err }
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil { return err }
	if role != "photo_scheduler" { return v2input.ErrInvalid }
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.v28_glare_completed_source_ready($1,$2,$3)", scope.Tenant, scope.Task, scope.Result).Scan(&ready); err != nil { return err }
	if !ready { return v2input.ErrExpired }
	if err := ctx.Err(); err != nil { return err }
	if err := tx.Commit(ctx); err != nil { return err }
	return ctx.Err()
}

type currentGlareAdoptionVerifier struct {
	Inner RestorationEvidenceVerifier
	Authority glareAdoptionAuthority
}

func (v currentGlareAdoptionVerifier) VerifyWithEvidence(ctx context.Context, who auth.Identity,
	scope RestorationArrayScope, operation string, params []byte,
) (RestorationReviewCandidate, int64, RestorationVerificationEvidence, error) {
	var candidate RestorationReviewCandidate
	var evidence RestorationVerificationEvidence
	if err := ctx.Err(); err != nil { return candidate, 0, evidence, err }
	if operation != "portrait.glare" || v.Inner == nil || v.Authority == nil { return candidate, 0, evidence, v2input.ErrInvalid }
	if err := v.Authority.Current(ctx, scope); err != nil { return candidate, 0, evidence, err }
	if err := ctx.Err(); err != nil { return candidate, 0, evidence, err }
	result, count, receipt, err := v.Inner.VerifyWithEvidence(ctx, who, scope, operation, params)
	if err != nil { return candidate, 0, evidence, err }
	if err := ctx.Err(); err != nil { return candidate, 0, evidence, err }
	if err := v.Authority.Current(ctx, scope); err != nil { return candidate, 0, evidence, err }
	if err := ctx.Err(); err != nil { return candidate, 0, evidence, err }
	return result, count, receipt, nil
}
