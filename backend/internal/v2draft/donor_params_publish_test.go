package v2draft

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"testing"
)

type donorIntentTx struct {
	donorDescriptorTestTx
	ready   bool
	writes  int
	request string
}
type donorIntentDB struct {
	tx     *donorIntentTx
	begins int
}

func (d *donorIntentDB) Begin(context.Context) (pgx.Tx, error) { d.begins++; return d.tx, nil }

type donorIntentRow struct{ ready bool }

func (r donorIntentRow) Scan(dest ...any) error { *(dest[0].(*bool)) = r.ready; return nil }
func (t *donorIntentTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT current_user" {
		return t.donorDescriptorTestTx.QueryRow(ctx, q, args...)
	}
	t.writes++
	t.request = args[2].(string)
	return donorIntentRow{t.ready}
}
func TestDonorParamsIntentCommitRequired(t *testing.T) {
	for _, mode := range []string{"ok", "role", "revoked", "commit", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			tx := &donorIntentTx{donorDescriptorTestTx: donorDescriptorTestTx{role: "photo_scheduler", failCommit: mode == "commit"}, ready: mode != "revoked"}
			if mode == "role" {
				tx.role = "photo_api"
			}
			db := &donorIntentDB{tx: tx}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			err := (SQLDonorParamsPublisher{DB: db}).saveIntent(ctx, uuid.New(), uuid.New(), []byte(`{"intent":"owned"}`))
			if (err == nil) != (mode == "ok") {
				t.Fatalf("err=%v", err)
			}
			if mode == "role" && tx.writes != 0 {
				t.Fatal("wrong role wrote intent")
			}
			if mode == "ok" && (tx.commits != 1 || tx.request != `{"intent":"owned"}`) {
				t.Fatal("not committed exactly")
			}
			if mode == "cancelled" {
				if db.begins != 0 {
					t.Fatal("cancelled intent began transaction")
				}
				return
			}
			if tx.rollbacks != 1 {
				t.Fatal("transaction cleanup")
			}
		})
	}
}
func TestDonorParamsRejectsUnqualifiedBeforeIO(t *testing.T) {
	db := &donorIntentDB{}
	s := SQLDonorParamsPublisher{DB: db, Publisher: artifactjson.Publisher{}}
	_, err := s.Publish(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), uuid.New(), 0, LoadedDonorTaskPlan{}, PreparedDonorInputEvidence{})
	if err == nil || db.begins != 0 {
		t.Fatal("unqualified input reached SQL")
	}
}
