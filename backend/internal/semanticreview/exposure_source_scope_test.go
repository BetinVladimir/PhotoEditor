package semanticreview

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"reflect"
	"testing"
)

type scopeFixtureTx struct {
	pgx.Tx
	calls  int
	values []any
	err    error
	args   []any
}
type scopeFixtureRow struct{ tx *scopeFixtureTx }

func (r scopeFixtureRow) Scan(dest ...any) error {
	if r.tx.err != nil {
		return r.tx.err
	}
	for i, value := range r.tx.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}
func (tx *scopeFixtureTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	tx.calls++
	tx.args = args
	return scopeFixtureRow{tx}
}
func TestExposureScopeOwnsRowsInputsAndFailsClosed(t *testing.T) {
	child, id := uuid.New(), uuid.New()
	raw, _ := json.Marshal([]ExposureTaskInputs{{TargetID: id}})
	values := []any{"NpyArray", id, "sha", int64(128), "bucket", "key", "version", "application/x-npy"}
	tx := &scopeFixtureTx{values: values}
	recorder := &exposureScopeRecorder{db: tx}
	args := []any{id, "body", int64(1), true}
	var contract, sha, bucket, key, version, media string
	var ref uuid.UUID
	var size int64
	if err := recorder.QueryRow(context.Background(), "SELECT fixture", args...).Scan(&contract, &ref, &sha, &size, &bucket, &key, &version, &media); err != nil {
		t.Fatal(err)
	}
	args[0] = uuid.New()
	version = "caller-mutated"
	scope := PreparedExposureSourceScope{inputs: raw, checks: recorder.checks, child: child}
	tx.calls = 0
	if err := scope.RecheckInTransaction(context.Background(), tx, child); err != nil || tx.calls != 1 || tx.args[0] != id {
		t.Fatal("scope projection changed", err)
	}
	copy, err := scope.Inputs()
	if err != nil {
		t.Fatal(err)
	}
	copy[0].TargetID = uuid.New()
	again, err := scope.Inputs()
	if err != nil || again[0].TargetID != id {
		t.Fatal("mutable scope inputs")
	}
	tx.values = append([]any(nil), values...)
	tx.values[6] = "different-version"
	if err := scope.RecheckInTransaction(context.Background(), tx, child); !errors.Is(err, ErrConflict) {
		t.Fatal("changed row accepted", err)
	}
	sentinel := errors.New("scope revoked")
	tx.err = sentinel
	if err := scope.RecheckInTransaction(context.Background(), tx, child); !errors.Is(err, sentinel) {
		t.Fatal("scope refusal lost", err)
	}
	before := tx.calls
	if err := scope.RecheckInTransaction(context.Background(), tx, uuid.New()); err == nil || tx.calls != before {
		t.Fatal("wrong child queried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scope.RecheckInTransaction(ctx, tx, child); err != context.Canceled || tx.calls != before {
		t.Fatal("cancel queried")
	}
	if _, err := (PreparedExposureSourceScope{}).Inputs(); err == nil {
		t.Fatal("zero scope accepted")
	}
}
