package taskrecovery

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

type recoveryTxRow struct{ err error }

func (r recoveryTxRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch p := dest[0].(type) {
	case *bool:
		*p = false
	case *int:
		*p = 0
	default:
		panic("unexpected recovery scalar")
	}
	return nil
}

type recoveryTxRows struct{ pgx.Rows }

func (recoveryTxRows) Next() bool { return false }
func (recoveryTxRows) Close()     {}
func (recoveryTxRows) Err() error { return nil }

type recoveryTxControl struct {
	pgx.Tx
	queries            []string
	failure            error
	commits, rollbacks int
}

func (t *recoveryTxControl) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	t.queries = append(t.queries, q)
	return recoveryTxRow{t.failure}
}
func (t *recoveryTxControl) Query(_ context.Context, q string, _ ...any) (pgx.Rows, error) {
	t.queries = append(t.queries, q)
	return recoveryTxRows{}, t.failure
}
func (t *recoveryTxControl) Commit(context.Context) error   { t.commits++; return nil }
func (t *recoveryTxControl) Rollback(context.Context) error { t.rollbacks++; return nil }

// A non-transaction pool-like querier has exactly the same production call flow.
type recoveryPoolControl struct{ tx *recoveryTxControl }

func (p recoveryPoolControl) QueryRow(c context.Context, q string, a ...any) pgx.Row {
	return p.tx.QueryRow(c, q, a...)
}
func (p recoveryPoolControl) Query(c context.Context, q string, a ...any) (pgx.Rows, error) {
	return p.tx.Query(c, q, a...)
}

var _ RecoveryDB = (*recoveryTxControl)(nil)

func TestRecoveryPoolAndCallerTransactionKeepSameFunctionSequence(t *testing.T) {
	tx := &recoveryTxControl{}
	pool := &recoveryTxControl{}
	for _, db := range []RecoveryDB{tx, recoveryPoolControl{pool}} {
		n, e := (Recovery{DB: db}).Recover(context.Background(), 1)
		if e != nil || n != 0 {
			t.Fatal("default no-work recovery changed", e)
		}
	}
	if len(tx.queries) == 0 || len(tx.queries) != len(pool.queries) {
		t.Fatal("recovery call sequence lost")
	}
	for i, q := range tx.queries {
		if q != pool.queries[i] {
			t.Fatal("pool/transaction algorithms differ")
		}
	}
	if tx.commits != 0 || tx.rollbacks != 0 {
		t.Fatal("recovery took caller transaction ownership")
	}
}
func TestRecoveryTransactionFailurePreservedForCallerRollback(t *testing.T) {
	failure := errors.New("source query failed")
	tx := &recoveryTxControl{failure: failure}
	_, e := (Recovery{DB: tx}).Recover(context.Background(), 1)
	if !errors.Is(e, failure) {
		t.Fatal("error lost")
	}
	_ = tx.Rollback(context.Background())
	if tx.commits != 0 || tx.rollbacks != 1 || len(tx.queries) != 1 {
		t.Fatal("failure not bounded and rolled back by owner")
	}
}
