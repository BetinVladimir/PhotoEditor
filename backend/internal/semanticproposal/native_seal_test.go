package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type nativeSealTx struct {
	pgx.Tx
	role            string
	raw             []byte
	calls           []string
	args            [][]any
	sqlErr, execErr error
}
type nativeSealRow struct {
	role string
	raw  []byte
	err  error
}

func (r nativeSealRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch v := dest[0].(type) {
	case *string:
		*v = r.role
	case *[]byte:
		*v = append([]byte(nil), r.raw...)
	default:
		panic("unexpected native seal scan")
	}
	return nil
}
func (tx *nativeSealTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	tx.calls = append(tx.calls, query)
	tx.args = append(tx.args, args)
	if query == "SELECT current_user" {
		return nativeSealRow{role: tx.role}
	}
	return nativeSealRow{raw: tx.raw, err: tx.sqlErr}
}
func (tx *nativeSealTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.calls = append(tx.calls, query)
	tx.args = append(tx.args, args)
	return pgconn.NewCommandTag("SELECT 1"), tx.execErr
}

func assertNativeSeal(t *testing.T, tenant, run, workflow uuid.UUID, seal func(context.Context, pgx.Tx) (v2input.JobTicket, error)) {
	t.Helper()
	expected := v2input.JobTicket{RunID: run, WorkflowID: workflow, State: "queued"}
	raw, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	tx := &nativeSealTx{role: "photo_scheduler", raw: raw}
	ticket, err := seal(context.Background(), tx)
	if err != nil || ticket != expected {
		t.Fatal("native receipt", err)
	}
	if len(tx.calls) != 3 || tx.calls[1] != "SELECT set_config('app.tenant_id',$1,true)" || tx.args[1][0] != tenant.String() || tx.calls[2] != "SELECT photo.seal_semantic_native_preparation($1,$2,$3::jsonb)" {
		t.Fatal("native seal authority order")
	}
	var packet nativeAdmissionEvidence
	args := tx.args[2]
	if args[0] != tenant || args[1] != run || json.Unmarshal([]byte(args[2].(string)), &packet) != nil || packet.Tenant != tenant || packet.Run != run {
		t.Fatal("native seal substituted packet")
	}
	for _, tc := range []struct {
		name string
		raw  []byte
		want error
	}{
		{"absent", nil, v2input.ErrConflict},
		{"malformed", []byte("{"), v2input.ErrInvalid},
		{"foreign", []byte(`{"run_id":"00000000-0000-0000-0000-000000000000","state":"queued"}`), v2input.ErrInvalid},
		{"wrong_state", []byte(`{"state":"draft"}`), v2input.ErrInvalid},
	} {
		tx = &nativeSealTx{role: "photo_scheduler", raw: tc.raw}
		if _, err := seal(context.Background(), tx); !errors.Is(err, tc.want) {
			t.Fatal(tc.name, err)
		}
	}
	tx = &nativeSealTx{role: "photo_api", raw: raw}
	if _, err := seal(context.Background(), tx); !errors.Is(err, v2input.ErrInvalid) || len(tx.calls) != 1 {
		t.Fatal("foreign role reached sealing", err)
	}
	dependency := errors.New("native test SQL unavailable")
	for _, execution := range []bool{false, true} {
		tx = &nativeSealTx{role: "photo_scheduler", raw: raw}
		if execution {
			tx.execErr = dependency
		} else {
			tx.sqlErr = dependency
		}
		if _, err := seal(context.Background(), tx); !errors.Is(err, dependency) {
			t.Fatal("lost native SQL error", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx = &nativeSealTx{role: "photo_scheduler", raw: raw}
	if _, err := seal(ctx, tx); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancel reached native SQL", err)
	}
	if _, err := seal(context.Background(), nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("nil transaction", err)
	}
}
