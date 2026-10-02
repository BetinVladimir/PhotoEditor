package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

type ingestQueries struct {
	calls  int
	failAt int
	ids    []uuid.UUID
}

func (q *ingestQueries) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.calls++
	q.ids = append(q.ids, args[2].(uuid.UUID))
	return expansionRow{values: []any{q.calls != q.failAt}}
}
func TestIngestPlanRegistrationResumesStableRows(t *testing.T) {
	spec, _ := ingestFixture(t)
	tenant := uuid.New()
	q := &ingestQueries{failAt: 3}
	repo := IngestPlanRepository{DB: q}
	plan, err := repo.Register(context.Background(), tenant, spec)
	if err == nil || len(plan.Nodes) != 0 || q.calls != 3 {
		t.Fatal("partial registration returned complete plan")
	}
	first := append([]uuid.UUID{}, q.ids...)
	q.calls = 0
	q.ids = nil
	q.failAt = 0
	plan, err = repo.Register(context.Background(), tenant, spec)
	if err != nil || len(plan.Nodes) != 6 || q.calls != 6 {
		t.Fatal("resume failed", err)
	}
	for i, id := range first {
		if q.ids[i] != id {
			t.Fatal("retry changed task identity")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q.calls = 0
	if _, err = repo.Register(ctx, tenant, spec); err != context.Canceled || q.calls != 0 {
		t.Fatal("cancelled registration issued SQL")
	}
}
