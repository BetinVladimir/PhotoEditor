package workflow

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
)

type expansionRow struct {
	values []any
	err    error
}

func (r expansionRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, v := range r.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type expansionQueries struct {
	row  expansionRow
	args []any
	sql  string
}

func (q *expansionQueries) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.args = args
	q.sql = sql
	return q.row
}

func TestExpansionLeaseAdapterAndFencedUpdates(t *testing.T) {
	input := expansionFixture(t)
	raw, err := canonical.Marshal(input.Selection)
	if err != nil {
		t.Fatal(err)
	}
	token, parent := uuid.New(), uuid.New()
	q := &expansionQueries{row: expansionRow{values: []any{input.ExpansionID, token, time.Now().Add(time.Minute), input.TenantID, input.RunID, input.ReviewID, parent, input.Resolved.SHA256, "repair", raw}}}
	repo := SQLExpansionLeases{DB: q}
	lease, err := repo.Claim(context.Background())
	if err != nil || lease == nil || lease.ExpansionID != input.ExpansionID || lease.Token != token || lease.Selection.Proposal != input.Selection.Proposal {
		t.Fatal("claim malformed", err)
	}
	q.row = expansionRow{values: []any{false}}
	done, err := repo.Renew(context.Background(), *lease)
	if err != nil || done || len(q.args) != 2 || q.args[0] != input.ExpansionID || q.args[1] != token {
		t.Fatal("renew ignored fencing", err)
	}
	q.row = expansionRow{values: []any{true}}
	if done, err = repo.Release(context.Background(), *lease); err != nil || !done {
		t.Fatal("release failed", err)
	}
	q.row = expansionRow{err: pgx.ErrNoRows}
	if lease, err = repo.Claim(context.Background()); err != nil || lease != nil {
		t.Fatal("empty queue is not error", err)
	}
}
func TestExpansionLeaseRejectsMissingTokenAndBadSelection(t *testing.T) {
	input := expansionFixture(t)
	raw, _ := canonical.Marshal(input.Selection)
	for _, bad := range []string{"token", "route", "selection"} {
		values := []any{input.ExpansionID, uuid.New(), time.Now(), input.TenantID, input.RunID, input.ReviewID, uuid.New(), input.Resolved.SHA256, "repair", raw}
		switch bad {
		case "token":
			values[1] = uuid.Nil
		case "route":
			values[8] = "unknown"
		case "selection":
			values[9] = []byte(`{"extra":true}`)
		}
		q := &expansionQueries{row: expansionRow{values: values}}
		if lease, err := (SQLExpansionLeases{DB: q}).Claim(context.Background()); err == nil || lease != nil {
			t.Fatal("invalid lease accepted", bad)
		}
	}
}
