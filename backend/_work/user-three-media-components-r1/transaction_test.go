package main

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
)

type controlRow func(...any) error

func (r controlRow) Scan(d ...any) error { return r(d...) }

type controlRows struct {
	pgx.Rows
	data  [][]any
	index int
}

func (r *controlRows) Next() bool { r.index++; return r.index <= len(r.data) }
func (r *controlRows) Scan(d ...any) error {
	for i, v := range r.data[r.index-1] {
		switch p := d[i].(type) {
		case *string:
			*p = v.(string)
		case *[]byte:
			*p = v.([]byte)
		default:
			panic("unexpected control scan")
		}
	}
	return nil
}
func (r *controlRows) Close()     {}
func (r *controlRows) Err() error { return nil }

type componentTx struct {
	pgx.Tx
	foreignRevision    int
	role               string
	calls              []string
	commits, rollbacks int
	selected           uuid.UUID
}

func (t *componentTx) Exec(_ context.Context, q string, _ ...any) (pgconn.CommandTag, error) {
	t.calls = append(t.calls, q)
	if q == "SET LOCAL ROLE photo_scheduler" {
		t.role = "photo_scheduler"
	}
	if q == "RESET ROLE" {
		t.role = "operator"
	}
	return pgconn.NewCommandTag("SET"), nil
}
func (t *componentTx) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	t.calls = append(t.calls, q)
	return controlRow(func(d ...any) error {
		switch {
		case strings.HasPrefix(q, "SELECT count(*)"):
			*d[0].(*int) = 0
		case q == "SELECT current_user":
			*d[0].(*string) = t.role
		case q == "SELECT photo.defer_dispatch($1,$2)":
			*d[0].(*bool) = true
		case q == "SELECT photo.finalize_media_retry_job()":
			if t.role != "photo_scheduler" {
				panic("wrong function role")
			}
			t.foreignRevision++
			*d[0].(*bool) = true
		case q == "SELECT task_id,resource_class,profile FROM photo.pick_ready_task()":
			*d[0].(*uuid.UUID) = t.selected
			*d[1].(*string) = "media"
			*d[2].(*[]byte) = []byte(`{}`)
		default:
			panic("unexpected reserve/defer/accounting SQL")
		}
		return nil
	})
}
func (t *componentTx) Query(_ context.Context, q string, _ ...any) (pgx.Rows, error) {
	t.calls = append(t.calls, q)
	if strings.Contains(q, "FROM pg_class") {
		return &controlRows{data: [][]any{{"tenants"}}}, nil
	}
	if strings.Contains(q, `"photo"."tenants"`) {
		raw, _ := json.Marshal(map[string]any{"id": "foreign-tenant", "revision": t.foreignRevision})
		return &controlRows{data: [][]any{{raw}}}, nil
	}
	panic("unexpected query")
}
func (t *componentTx) Commit(context.Context) error   { t.commits++; return nil }
func (t *componentTx) Rollback(context.Context) error { t.rollbacks++; return nil }
func TestActualNormalFinalizerForeignEffectRollsBackWholeCallerTransaction(t *testing.T) {
	tx := &componentTx{}
	n, e := one(context.Background(), tx, fixture(t), "finalize")
	if e != nil {
		_ = tx.Rollback(context.Background())
	} else {
		_ = tx.Commit(context.Background())
	}
	if e == nil || n != 0 || tx.foreignRevision != 1 || tx.rollbacks != 1 || tx.commits != 0 || tx.role != "operator" {
		t.Fatal("normal finalizer foreign delta committed or hidden", e)
	}
	found := false
	for _, q := range tx.calls {
		if q == "SELECT photo.finalize_media_retry_job()" {
			found = true
		}
	}
	if !found {
		t.Fatal("production finalizer not invoked")
	}
}
func TestActualDispatcherForeignSelectionRollsBackBeforeReserveOrRecord(t *testing.T) {
	tx := &componentTx{selected: uuid.New()}
	_, e := one(context.Background(), tx, fixture(t), "dispatch")
	if e != nil {
		_ = tx.Rollback(context.Background())
	} else {
		_ = tx.Commit(context.Background())
	}
	if e == nil || tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatal("foreign dispatch not rolled back")
	}
	for _, q := range tx.calls {
		if strings.Contains(q, "reserve_dispatch") || strings.Contains(q, "record_dispatch") || strings.Contains(q, "defer_dispatch") {
			t.Fatal("admission before scope acceptance")
		}
	}
}
