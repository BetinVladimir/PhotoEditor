package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
	"testing"
)

type downloadOutcomeRows struct {
	pgx.Rows
	values [][]any
	i      int
	closed bool
}

func (r *downloadOutcomeRows) Close()     { r.closed = true }
func (r *downloadOutcomeRows) Err() error { return nil }
func (r *downloadOutcomeRows) Next() bool { return r.i < len(r.values) }
func (r *downloadOutcomeRows) Scan(dest ...any) error {
	for i, x := range r.values[r.i] {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(x))
	}
	r.i++
	return nil
}

type downloadOutcomeDB struct{ rows *downloadOutcomeRows }

func (d downloadOutcomeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return d.rows, nil
}

func TestDownloadEncodeOutcomesWaitAndValidateCanonicalValues(t *testing.T) {
	tenant, run, version, artifact := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	rows := &downloadOutcomeRows{values: [][]any{{int64(0), version, "ready", (*uuid.UUID)(nil), (*string)(nil), (*int64)(nil), (*string)(nil)}}}
	items, ready, err := (DownloadEncodeOutcomeSource{DB: downloadOutcomeDB{rows}}).Load(context.Background(), tenant, run)
	if err != nil || ready || items != nil || !rows.closed {
		t.Fatal(items, ready, err)
	}
	rows = &downloadOutcomeRows{values: [][]any{{int64(0), version, "succeeded", &artifact, &sha, func() *int64 { x := int64(20); return &x }(), func() *string { x := "photo.jpg"; return &x }()}}}
	items, ready, err = (DownloadEncodeOutcomeSource{DB: downloadOutcomeDB{rows}}).Load(context.Background(), tenant, run)
	if err != nil || !ready || len(items) != 1 || items[0].ArtifactID != artifact {
		t.Fatal(items, ready, err)
	}
}

func TestDownloadPackagingPersistsCanonicalPlan(t *testing.T) {
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	source := downloadSource(0, 5, "photo.jpg", strings.Repeat("a", 64))
	plan, _ := PlanDownloadVolumes([]DownloadSource{source}, 10)
	ok, err := (DownloadPackagingRepository{DB: q}).Begin(context.Background(), uuid.New(), uuid.New(), plan)
	if err != nil || !ok || len(q.args) != 3 {
		t.Fatal(ok, err, q.args)
	}
}
