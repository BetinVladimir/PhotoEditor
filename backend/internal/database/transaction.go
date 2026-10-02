package database

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InTransaction retries only complete DB-only callbacks. Never perform external
// effects in fn: a serialization retry may invoke it again.
func InTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	for attempt := 0; attempt < 3; attempt++ {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		err = func() error {
			defer tx.Rollback(context.WithoutCancel(ctx))
			if callbackErr := fn(tx); callbackErr != nil {
				return callbackErr
			}
			return tx.Commit(ctx)
		}()
		if err == nil {
			return nil
		}
		_ = tx.Rollback(ctx)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || (pe.Code != "40001" && pe.Code != "40P01") || attempt == 2 {
			return err
		}
		timer := time.NewTimer(time.Duration(10*(attempt+1)+rand.IntN(10)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

// TenantTransaction establishes transaction-local context after authorization.
// tenant is never derived from an unverified token or request header alone.
func TenantTransaction(ctx context.Context, pool *pgxpool.Pool, tenant uuid.UUID, fn func(pgx.Tx) error) error {
	if tenant == uuid.Nil {
		return errors.New("tenant context required")
	}
	return InTransaction(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
			return err
		}
		return fn(tx)
	})
}
