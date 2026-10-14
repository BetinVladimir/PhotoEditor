package v2draft

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"testing"
)

type donorDescriptorTestDB struct {
	tx     *donorDescriptorTestTx
	begins int
}

func (d *donorDescriptorTestDB) Begin(context.Context) (pgx.Tx, error) { d.begins++; return d.tx, nil }

type donorDescriptorTestTx struct {
	pgx.Tx
	role                      string
	raw                       []byte
	commits, rollbacks, calls int
	failCommit                bool
	tenant                    string
}
type donorDescriptorTestRow struct{ tx *donorDescriptorTestTx }

func (r donorDescriptorTestRow) Scan(dest ...any) error {
	switch value := dest[0].(type) {
	case *string:
		*value = r.tx.role
	case *[]byte:
		*value = append([]byte(nil), r.tx.raw...)
	}
	return nil
}
func (t *donorDescriptorTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	t.calls++
	return donorDescriptorTestRow{t}
}
func (t *donorDescriptorTestTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	t.tenant = args[0].(string)
	return pgconn.CommandTag{}, nil
}
func (t *donorDescriptorTestTx) Commit(context.Context) error {
	t.commits++
	if t.failCommit {
		return errors.New("commit")
	}
	return nil
}
func (t *donorDescriptorTestTx) Rollback(context.Context) error { t.rollbacks++; return nil }
func TestDonorDescriptorReadTransaction(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, run := uuid.New(), uuid.New()
	for _, mode := range []string{"ok", "role", "null", "noncanonical", "commit", "cancelled"} {
		tx := &donorDescriptorTestTx{role: "photo_scheduler", raw: []byte(`{"schema_version":1}`), failCommit: mode == "commit"}
		switch mode {
		case "role":
			tx.role = "photo_api"
		case "null":
			tx.raw = nil
		case "noncanonical":
			tx.raw = append([]byte(" "), tx.raw...)
		}
		db := &donorDescriptorTestDB{tx: tx}
		store := SQLDonorDescriptorStore{DB: db}
		ctx := context.Background()
		if mode == "cancelled" {
			c, cancel := context.WithCancel(ctx)
			cancel()
			ctx = c
		}
		got, err := store.Read(ctx, who, tenant, run)
		if mode == "ok" {
			if err != nil || string(got) != string(tx.raw) || tx.commits != 1 || tx.tenant != tenant.String() {
				t.Fatal("read transaction", err)
			}
			got[0] ^= 1
			if tx.raw[0] != '{' {
				t.Fatal("mutable raw")
			}
		} else if err == nil || got != nil {
			t.Fatal("partial read", mode, err)
		}
		if mode != "ok" && mode != "commit" && tx.commits != 0 {
			t.Fatal("failed read committed", mode)
		}
		if mode == "cancelled" && db.begins != 0 {
			t.Fatal("cancelled read began")
		}
	}
	db := &donorDescriptorTestDB{tx: &donorDescriptorTestTx{}}
	if err := (SQLDonorDescriptorStore{DB: db}).Save(context.Background(), who, tenant, run, FrozenDonorTask{}); err == nil || db.begins != 0 {
		t.Fatal("unqualified descriptor reached SQL")
	}
	if err := (SQLDonorDescriptorStore{DB: db}).Stage(context.Background(), who, tenant, run, FrozenDonorTask{}); err == nil || db.begins != 0 {
		t.Fatal("unqualified staging reached SQL")
	}

}
