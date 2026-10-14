package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDraftRetryReturnsOnlyCommittedAdmission(t *testing.T) {
	attempts, checks := 0, 0
	stale := DraftAdmission{RunID: uuid.New(), Outcome: "created"}
	final := DraftAdmission{RunID: uuid.New(), Outcome: "pending"}
	result, err := beginDraftWithRetry(context.Background(), func(context.Context) (pgx.Tx, error) {
		attempts++
		if attempts == 1 {
			return &retryTestTx{commitErr: &pgconn.PgError{Code: "40001"}}, nil
		}
		return &retryTestTx{}, nil
	}, func(pgx.Tx) (DraftAdmission, error) {
		checks++
		if checks == 1 {
			return stale, nil
		}
		return final, nil
	})
	if err != nil || attempts != 2 || checks != 2 || result != final {
		t.Fatal("aborted admission escaped", result, err, attempts, checks)
	}
}
func TestDraftFailureNeverReturnsAdmission(t *testing.T) {
	for _, failure := range []error{errors.New("commit outcome unknown"), v2input.ErrConflict} {
		attempts := 0
		result, err := beginDraftWithRetry(context.Background(), func(context.Context) (pgx.Tx, error) { attempts++; return &retryTestTx{commitErr: failure}, nil }, func(pgx.Tx) (DraftAdmission, error) {
			return DraftAdmission{RunID: uuid.New(), Outcome: "created"}, nil
		})
		if !errors.Is(err, failure) || result != (DraftAdmission{}) || attempts != 1 {
			t.Fatal("uncommitted admission returned", result, err, attempts)
		}
	}
	checks := 0
	result, err := beginDraftWithRetry(context.Background(), func(context.Context) (pgx.Tx, error) { return &retryTestTx{}, nil }, func(pgx.Tx) (DraftAdmission, error) {
		checks++
		if checks == 1 {
			return DraftAdmission{RunID: uuid.New()}, &pgconn.PgError{Code: "40P01"}
		}
		return DraftAdmission{RunID: uuid.New()}, v2input.ErrConflict
	})
	if err != v2input.ErrConflict || result != (DraftAdmission{}) || checks != 2 {
		t.Fatal("partial failed admission escaped", result, err, checks)
	}
}
