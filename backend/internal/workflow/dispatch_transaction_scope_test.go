package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

type scopeDispatchRow func(...any) error

func (r scopeDispatchRow) Scan(d ...any) error { return r(d...) }

type scopeDispatchTx struct {
	pgx.Tx
	id                    uuid.UUID
	profile               []byte
	queries               []string
	err                   error
	rolledBack, committed bool
	deferReason           string
}

func (t *scopeDispatchTx) QueryRow(_ context.Context, q string, a ...any) pgx.Row {
	t.queries = append(t.queries, q)
	return scopeDispatchRow(func(d ...any) error {
		if q == "SELECT task_id,resource_class,profile FROM photo.pick_ready_task()" || q == "SELECT task_id,resource_class,profile FROM photo.pick_ready_legacy_task()" {
			if t.err != nil {
				return t.err
			}
			*d[0].(*uuid.UUID) = t.id
			*d[1].(*string) = "media"
			*d[2].(*[]byte) = t.profile
			return nil
		}
		if q == "SELECT photo.defer_dispatch($1,$2)" {
			t.deferReason = a[1].(string)
			*d[0].(*bool) = true
			return nil
		}
		panic("unexpected reserve/record query")
	})
}
func (t *scopeDispatchTx) Rollback(context.Context) error { t.rolledBack = true; return nil }
func (t *scopeDispatchTx) Commit(context.Context) error   { t.committed = true; return nil }
func TestDispatchScopeForeignSelectionRollsBackBeforeAdmission(t *testing.T) {
	tx := &scopeDispatchTx{id: uuid.New(), profile: []byte(`{}`)}
	refused := errors.New("foreign scope")
	_, queued, err := (Dispatcher{}).DispatchOneInTransaction(context.Background(), tx, func(uuid.UUID, string, []byte) error { return refused })
	// This is the required caller-owned transaction boundary.
	if err != nil {
		_ = tx.Rollback(context.Background())
	} else {
		_ = tx.Commit(context.Background())
	}
	if !errors.Is(err, refused) || queued || !tx.rolledBack || tx.committed || len(tx.queries) != 1 {
		t.Fatal("foreign selection admitted or committed")
	}
}
func TestDispatchScopeValidatorCannotMutateAdmissionProfile(t *testing.T) {
	tx := &scopeDispatchTx{id: uuid.New(), profile: []byte(`invalid`)}
	picked, queued, err := (Dispatcher{}).DispatchOneInTransaction(context.Background(), tx, func(id uuid.UUID, class string, p []byte) error {
		if id != tx.id || class != "media" {
			t.Fatal("selection lost")
		}
		copy(p, []byte(`changed`))
		return nil
	})
	if err != nil || !picked || queued || string(tx.profile) != "invalid" || tx.deferReason != "invalid_resource_estimate" || len(tx.queries) != 2 {
		t.Fatal("profile mutation changed ordinary defer path", err)
	}
}
func TestDispatchDefaultNoRowAndLegacySelectionRemainUnchanged(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		tx := &scopeDispatchTx{err: pgx.ErrNoRows}
		picked, queued, err := (Dispatcher{LegacyOnly: legacy}).dispatchInTransaction(context.Background(), tx)
		want := "SELECT task_id,resource_class,profile FROM photo.pick_ready_task()"
		if legacy {
			want = "SELECT task_id,resource_class,profile FROM photo.pick_ready_legacy_task()"
		}
		if err != nil || picked || queued || len(tx.queries) != 1 || tx.queries[0] != want {
			t.Fatal("default selection changed")
		}
	}
}
func TestDispatchTransactionContextAndNilRefusedBeforeSQL(t *testing.T) {
	tx := &scopeDispatchTx{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := (Dispatcher{}).DispatchOneInTransaction(ctx, tx, nil); !errors.Is(e, context.Canceled) || len(tx.queries) != 0 {
		t.Fatal("cancelled selection queried")
	}
	if _, _, e := (Dispatcher{}).DispatchOneInTransaction(context.Background(), nil, nil); e == nil {
		t.Fatal("nil transaction accepted")
	}
}
