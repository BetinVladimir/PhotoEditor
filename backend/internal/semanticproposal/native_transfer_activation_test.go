package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/v2input"
)

func TestNativeTicketUnknownCommitReturnsNoTicketAndDoesNotBlindlyRetry(t *testing.T) {
	failure := errors.New("commit outcome unknown")
	calls := 0
	ticket, err := nativeTicketWithRetry(context.Background(), func(context.Context) (pgx.Tx, error) {
		calls++
		return &retryTestTx{commitErr: failure}, nil
	}, func(pgx.Tx) (v2input.JobTicket, error) {
		return v2input.JobTicket{RunID: uuid.New(), WorkflowID: uuid.New(), State: "queued"}, nil
	})
	if !errors.Is(err, failure) || ticket != (v2input.JobTicket{}) || calls != 1 {
		t.Fatal("unknown admission escaped", ticket, err, calls)
	}
}

func TestNativeTicketAbortedCommitRechecksAndReturnsOnlyFinalTicket(t *testing.T) {
	attempts, checks := 0, 0
	final := v2input.JobTicket{RunID: uuid.New(), WorkflowID: uuid.New(), State: "queued"}
	ticket, err := nativeTicketWithRetry(context.Background(), func(context.Context) (pgx.Tx, error) {
		attempts++
		if attempts == 1 {
			return &retryTestTx{commitErr: &pgconn.PgError{Code: "40001"}}, nil
		}
		return &retryTestTx{}, nil
	}, func(pgx.Tx) (v2input.JobTicket, error) {
		checks++
		if checks == 1 {
			return v2input.JobTicket{RunID: uuid.New(), WorkflowID: uuid.New(), State: "queued"}, nil
		}
		return final, nil
	})
	if err != nil || ticket != final || attempts != 2 || checks != 2 {
		t.Fatal("aborted ticket escaped", ticket, err, attempts, checks)
	}
}

func TestNativeTicketAbortedWorkThenRefusalDiscardsEarlierTicket(t *testing.T) {
	failure := v2input.ErrExpired
	attempts, checks := 0, 0
	ticket, err := nativeTicketWithRetry(context.Background(), func(context.Context) (pgx.Tx, error) {
		attempts++
		return &retryTestTx{}, nil
	}, func(pgx.Tx) (v2input.JobTicket, error) {
		checks++
		if checks == 1 {
			return v2input.JobTicket{RunID: uuid.New(), WorkflowID: uuid.New(), State: "queued"}, &pgconn.PgError{Code: "40P01"}
		}
		return v2input.JobTicket{}, failure
	})
	if !errors.Is(err, failure) || ticket != (v2input.JobTicket{}) || attempts != 2 || checks != 2 {
		t.Fatal("refused retry retained a ticket", ticket, err, attempts, checks)
	}
}
