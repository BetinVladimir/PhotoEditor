package taskrecovery

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

type quotaRow struct {
	changed bool
	err     error
}

func (r quotaRow) Scan(values ...any) error {
	if r.err != nil {
		return r.err
	}
	*values[0].(*bool) = r.changed
	return nil
}

type quotaDB struct {
	queries         []string
	historicalCalls int
	fail            error
}

func (db *quotaDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	db.queries = append(db.queries, query)
	if query == "SELECT photo.release_next_v23_cancelled_training_quota()" {
		db.historicalCalls++
		if db.fail != nil {
			return quotaRow{err: db.fail}
		}
		return quotaRow{changed: db.historicalCalls == 1}
	}
	return quotaRow{}
}
func TestConsentQuotaSettlementMountCountsOnlyActualRepair(t *testing.T) {
	db := &quotaDB{}
	n, err := settleConsentQuotas(context.Background(), db, 3)
	if err != nil || n != 1 || db.historicalCalls != 2 || len(db.queries) != 4 {
		t.Fatalf("settlement n=%d calls=%d queries=%v err=%v", n, db.historicalCalls, db.queries, err)
	}
}
func TestConsentQuotaSettlementDoesNotMaskHistoricalFailure(t *testing.T) {
	expected := errors.New("repair refused")
	db := &quotaDB{fail: expected}
	n, err := settleConsentQuotas(context.Background(), db, 3)
	if n != 0 || !errors.Is(err, expected) || db.historicalCalls != 1 {
		t.Fatalf("n=%d err=%v calls=%d", n, err, db.historicalCalls)
	}
}
