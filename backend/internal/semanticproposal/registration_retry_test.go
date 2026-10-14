package semanticproposal

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type retryTestTx struct {
	pgx.Tx
	committed, rolledBack  bool
	commitErr, rollbackErr error
}

func (t *retryTestTx) Commit(context.Context) error   { t.committed = true; return t.commitErr }
func (t *retryTestTx) Rollback(context.Context) error { t.rolledBack = true; return t.rollbackErr }

func TestRegistrationRetriesAbortedTransaction(t *testing.T) {
	for _, code := range []string{"40P01", "40001"} {
		t.Run(code, func(t *testing.T) {
			var transactions []*retryTestTx
			begins := func(context.Context) (pgx.Tx, error) {
				tx := &retryTestTx{}
				transactions = append(transactions, tx)
				return tx, nil
			}
			checks := 0
			err := retryProposalTransaction(context.Background(), begins, func(tx pgx.Tx) error {
				checks++
				if checks == 1 {
					return &pgconn.PgError{Code: code}
				}
				if !transactions[0].rolledBack || tx == transactions[0] {
					t.Fatal("retry reused unrolled transaction")
				}
				return nil
			})
			if err != nil || checks != 2 || len(transactions) != 2 || transactions[0].committed || !transactions[1].committed {
				t.Fatal("whole transaction retry", err, checks)
			}
		})
	}
}
func TestRegistrationDoesNotRetryUnknownOutcomeOrConflict(t *testing.T) {
	for _, failure := range []error{errors.New("connection lost during commit"), v2input.ErrConflict, &pgconn.PgError{Code: "42501"}} {
		attempts := 0
		tx := &retryTestTx{commitErr: failure}
		err := retryProposalTransaction(context.Background(), func(context.Context) (pgx.Tx, error) { attempts++; return tx, nil }, func(pgx.Tx) error { return nil })
		if !errors.Is(err, failure) || attempts != 1 || !tx.rolledBack {
			t.Fatal("unsafe retry", err, attempts)
		}
	}
}
func TestRegistrationRetryLimitAndCleanupFailure(t *testing.T) {
	attempts := 0
	failure := &pgconn.PgError{Code: "40P01"}
	err := retryProposalTransaction(context.Background(), func(context.Context) (pgx.Tx, error) { attempts++; return &retryTestTx{}, nil }, func(pgx.Tx) error { return failure })
	if err != failure || attempts != 3 {
		t.Fatal("retry unbounded", err, attempts)
	}
	cleanup := errors.New("rollback failed")
	attempts = 0
	err = retryProposalTransaction(context.Background(), func(context.Context) (pgx.Tx, error) { attempts++; return &retryTestTx{rollbackErr: cleanup}, nil }, func(pgx.Tx) error { return failure })
	if !errors.Is(err, cleanup) || attempts != 1 {
		t.Fatal("retry after failed cleanup", err, attempts)
	}
}
func TestRegistrationRetryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	tx := &retryTestTx{}
	err := retryProposalTransaction(ctx, func(context.Context) (pgx.Tx, error) { attempts++; return tx, nil }, func(pgx.Tx) error { cancel(); return &pgconn.PgError{Code: "40P01"} })
	if !errors.Is(err, context.Canceled) || attempts != 1 || !tx.rolledBack {
		t.Fatal("cancelled retry continued", err, attempts)
	}
}

func TestRegistrationRetriesConfirmedCommitAbort(t *testing.T) {
	first := &retryTestTx{commitErr: &pgconn.PgError{Code: "40001"}, rollbackErr: pgx.ErrTxClosed}
	second := &retryTestTx{}
	attempts, checks := 0, 0
	err := retryProposalTransaction(context.Background(), func(context.Context) (pgx.Tx, error) {
		attempts++
		if attempts == 1 {
			return first, nil
		}
		if !first.rolledBack {
			t.Fatal("new transaction before cleanup")
		}
		return second, nil
	}, func(pgx.Tx) error { checks++; return nil })
	if err != nil || attempts != 2 || checks != 2 || !first.committed || !second.committed {
		t.Fatal("commit abort retry", err, attempts, checks)
	}
}
