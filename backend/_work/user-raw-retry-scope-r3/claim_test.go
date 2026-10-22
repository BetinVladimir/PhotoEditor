package retryscope

import (
	"context"
	"errors"
	"testing"
)

type fakeTx struct {
	commits, rollbacks int
	commitError        error
	cleanupLive        bool
}

func (t *fakeTx) Commit(context.Context) error { t.commits++; return t.commitError }
func (t *fakeTx) Rollback(c context.Context) error {
	t.rollbacks++
	t.cleanupLive = c.Err() == nil
	return nil
}
func TestCheckedClaimKeepsForeignNilAndErrorsUncommitted(t *testing.T) {
	for _, mode := range []string{"foreign", "nil", "claim_error", "commit_error", "success"} {
		t.Run(mode, func(t *testing.T) {
			tx := &fakeTx{}
			if mode == "commit_error" {
				tx.commitError = errors.New("uncertain commit")
			}
			value := 9
			lease, e := CheckedClaim(context.Background(), func(context.Context) (Transaction, error) { return tx, nil }, func(Transaction) (*int, error) {
				if mode == "nil" {
					return nil, nil
				}
				if mode == "claim_error" {
					return nil, errors.New("claim failed")
				}
				return &value, nil
			}, func(*int) bool { return mode != "foreign" })
			if tx.rollbacks != 1 || !tx.cleanupLive {
				t.Fatal("rollback cleanup missing")
			}
			if mode == "success" {
				if e != nil || lease == nil || tx.commits != 1 {
					t.Fatal("validated lease did not commit")
				}
			} else if mode == "commit_error" {
				if e == nil || lease != nil || tx.commits != 1 {
					t.Fatal("commit error exposed lease")
				}
			} else {
				if tx.commits != 0 || lease != nil {
					t.Fatal("unvalidated lease committed")
				}
				if mode != "nil" && e == nil {
					t.Fatal("error missing")
				}
			}
		})
	}
}
func TestCheckedClaimBeginFailureDoesNotClaim(t *testing.T) {
	_, e := CheckedClaim(context.Background(), func(context.Context) (Transaction, error) { return nil, errors.New("begin") }, func(Transaction) (*int, error) { t.Fatal("claim called"); return nil, nil }, func(*int) bool { return true })
	if e == nil {
		t.Fatal("begin error swallowed")
	}
}
func TestCheckedClaimCancellationStillRollsBackWithLiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &fakeTx{}
	_, _ = CheckedClaim(ctx, func(context.Context) (Transaction, error) { return tx, nil }, func(Transaction) (*int, error) { return nil, ctx.Err() }, func(*int) bool { return false })
	if tx.commits != 0 || tx.rollbacks != 1 || !tx.cleanupLive {
		t.Fatal("cancelled cleanup not bounded independently")
	}
}
