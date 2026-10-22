package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

type authoredSettlementRow struct {
	value bool
	err   error
	after func()
}

func (r authoredSettlementRow) Scan(dst ...any) error {
	if r.after != nil {
		r.after()
	}
	if r.err != nil {
		return r.err
	}
	*dst[0].(*bool) = r.value
	return nil
}

type authoredSettlementQueries struct {
	rows           []authoredSettlementRow
	calls          []string
	tenant, parent uuid.UUID
	t              *testing.T
}

func (q *authoredSettlementQueries) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	q.t.Helper()
	if len(args) != 2 || args[0] != q.tenant || args[1] != q.parent {
		q.t.Fatal("foreign settlement scope")
	}
	q.calls = append(q.calls, sql)
	r := q.rows[0]
	q.rows = q.rows[1:]
	return r
}
func TestAuthoredReviewSettlementStatementOrderAndIdempotency(t *testing.T) {
	for _, v := range []struct{ a, b, want bool }{{true, true, true}, {false, true, true}, {true, false, true}, {false, false, false}} {
		q := &authoredSettlementQueries{rows: []authoredSettlementRow{{value: v.a}, {value: v.b}}, tenant: uuid.New(), parent: uuid.New(), t: t}
		got, err := settleAuthoredReviewStatements(context.Background(), q, q.tenant, q.parent)
		if err != nil || got != v.want || len(q.calls) != 2 || !strings.Contains(q.calls[0], "settle_authored_render_review(") || !strings.Contains(q.calls[1], "settle_authored_render_base_quota(") {
			t.Fatal("settlement sequence", got, err, q.calls)
		}
	}
}
func TestAuthoredReviewSettlementPreservesCancellationAndErrors(t *testing.T) {
	sentinel := errors.New("injected settlement failure")
	for _, at := range []int{0, 1} {
		q := &authoredSettlementQueries{rows: []authoredSettlementRow{{value: true}, {value: true}}, tenant: uuid.New(), parent: uuid.New(), t: t}
		q.rows[at].err = sentinel
		got, err := settleAuthoredReviewStatements(context.Background(), q, q.tenant, q.parent)
		if got || !errors.Is(err, sentinel) || len(q.calls) != at+1 {
			t.Fatal(got, err, q.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &authoredSettlementQueries{rows: []authoredSettlementRow{{value: true, after: cancel}, {value: true}}, tenant: uuid.New(), parent: uuid.New(), t: t}
	got, err := settleAuthoredReviewStatements(ctx, q, q.tenant, q.parent)
	if got || !errors.Is(err, context.Canceled) || len(q.calls) != 1 {
		t.Fatal(got, err, q.calls)
	}
	if got, err := (AuthoredPhaseStore{}).SettleReview(ctx, q.tenant, q.parent); got || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
}
