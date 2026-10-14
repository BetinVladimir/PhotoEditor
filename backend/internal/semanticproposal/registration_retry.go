package semanticproposal

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

// Retry only server-confirmed aborted transactions. Unknown commit outcomes,
// connection failures and application conflicts are left to durable replay.
// Work must contain no external publication and must recheck all authority.
func retryProposalTransaction(ctx context.Context, begin func(context.Context) (pgx.Tx, error), work func(pgx.Tx) error) error {
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := begin(ctx)
		if err != nil {
			return err
		}
		err = work(tx)
		if err == nil {
			err = tx.Commit(ctx)
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rollbackErr := tx.Rollback(cleanup)
		cancel()
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, rollbackErr)
		}
		if err == nil {
			return nil
		}
		var postgres *pgconn.PgError
		if attempt == 2 || !errors.As(err, &postgres) || (postgres.Code != "40P01" && postgres.Code != "40001") {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable registration retry")
}
