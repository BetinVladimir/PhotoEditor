package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// AuthoredPhaseStore consumes completed canonical producer observations, never
// a caller's output collection. The native producer's own independent adoption
// must have completed before SQL will append the phase. Params publication and
// ordinary worker dispatch remain the producer's guarded responsibilities.
type AuthoredPhaseStore struct{ Scheduler *pgxpool.Pool }

func (s AuthoredPhaseStore) RecordCanonical(ctx context.Context, source AuthoredInstructionSource, task AuthoredPhaseTask,
	output v2input.SessionVersionBinding, result, report v2input.Ref, geometry *AuthoredGeometryPhaseWitness, physicalClosure []v2input.Ref) (bool, error) {
	if s.Scheduler == nil {
		return false, v2input.ErrInvalid
	}
	raw, _, err := CompileAuthoredPhaseReceipt(source, task, output, result, report, geometry, physicalClosure)
	if err != nil {
		return false, err
	}
	var accepted bool
	if err = s.Scheduler.QueryRow(ctx, `SELECT photo.commit_authored_render_phase($1,$2,$3)`, source.Tenant, source.Parent, string(raw)).Scan(&accepted); err != nil {
		return false, err
	}
	return accepted, nil
}

// SettleReview atomically publishes review availability and releases only the
// independently proven reserved remainder. The two SQL statements remain
// separate so the STABLE readiness predicate sees this transaction's prior
// status update. Cancellation/error rolls back both; no user approval occurs.
func (s AuthoredPhaseStore) SettleReview(ctx context.Context, tenant, parent uuid.UUID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.Scheduler == nil || tenant == uuid.Nil || parent == uuid.Nil {
		return false, v2input.ErrInvalid
	}
	var changed bool
	err := database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
		var err error
		changed, err = settleAuthoredReviewStatements(ctx, tx, tenant, parent)
		return err
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}
func settleAuthoredReviewStatements(ctx context.Context, tx artifactjson.RowQuerier, tenant, parent uuid.UUID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var review, quota bool
	if err := tx.QueryRow(ctx, `SELECT photo.settle_authored_render_review($1,$2)`, tenant, parent).Scan(&review); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := tx.QueryRow(ctx, `SELECT photo.settle_authored_render_base_quota($1,$2)`, tenant, parent).Scan(&quota); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return review || quota, nil
}
