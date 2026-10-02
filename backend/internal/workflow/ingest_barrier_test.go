package workflow

import (
	"context"
	"github.com/google/uuid"
	"reflect"
	"testing"
)

func TestIngestInspectionBarrierAdapter(t *testing.T) {
	tenant, run := uuid.New(), uuid.New()
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	repo := IngestPlanRepository{DB: q}
	sealed, err := repo.SealInspection(context.Background(), tenant, run)
	if err != nil || !sealed || !reflect.DeepEqual(q.args, []any{tenant, run}) {
		t.Fatal("barrier scope mismatch", err)
	}
	q.row = expansionRow{values: []any{false}}
	if sealed, err = repo.SealInspection(context.Background(), tenant, run); err != nil || sealed {
		t.Fatal("incomplete graph sealed")
	}
	q.args = nil
	if _, err = repo.SealInspection(context.Background(), uuid.Nil, run); err == nil || q.args != nil {
		t.Fatal("invalid scope queried")
	}
}
