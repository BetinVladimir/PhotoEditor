package semanticproposal

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

// Two real transactions create an advisory-lock cycle. The short-timeout victim
// must rollback and re-enter work with fresh transaction-local state.
func probeRegistrationDeadlockRetry(ctx context.Context, owner *pgxpool.Pool, t *testing.T) {
	t.Helper()
	blocker, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	var pid int
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, "SET LOCAL deadlock_timeout='5s'"); err != nil {
		t.Fatal(err)
	}
	prefix := "photo.registration.retry:" + uuid.NewString()
	a, b := prefix+":a", prefix+":b"
	lock := "SELECT pg_advisory_xact_lock(hashtextextended($1,0))"
	if _, err := blocker.Exec(ctx, lock, b); err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	attempts, aborts := 0, 0
	err = retryProposalTransaction(ctx, owner.Begin, func(tx pgx.Tx) error {
		attempts++
		var state string
		if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('photo.registration_retry_probe',true),'')").Scan(&state); err != nil {
			return err
		}
		if state != "" {
			return fmt.Errorf("aborted local transaction state retained")
		}
		if _, err := tx.Exec(ctx, "SELECT set_config('photo.registration_retry_probe','dirty',true)"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SET LOCAL deadlock_timeout='50ms'"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, lock, a); err != nil {
			return err
		}
		if attempts == 1 {
			go func() {
				_, err := blocker.Exec(ctx, lock, a)
				if err == nil {
					err = blocker.Commit(ctx)
				}
				completed <- err
			}()
			// Wait for the actual second connection lock request, not goroutine launch.
			for {
				var waiting bool
				if err := owner.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)", pid).Scan(&waiting); err != nil {
					return err
				}
				if waiting {
					break
				}
				timer := time.NewTimer(5 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
		_, err := tx.Exec(ctx, lock, b)
		var postgres *pgconn.PgError
		if errors.As(err, &postgres) && postgres.Code == "40P01" {
			aborts++
		}
		return err
	})
	if err != nil || attempts != 2 || aborts != 1 {
		t.Fatal("physical transaction retry", err, attempts, aborts)
	}
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal("deadlock survivor", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	t.Log("V27_REGISTRATION_PHYSICAL_DEADLOCK_RETRY_OK")
}
