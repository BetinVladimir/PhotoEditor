package v2optical

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

type negativeLockRow func(...any) error

func (r negativeLockRow) Scan(dest ...any) error { return r(dest...) }

type negativeLockTx struct {
	pgx.Tx
	mode   string
	calls  []string
	tenant uuid.UUID
}

func (t *negativeLockTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	t.calls = append(t.calls, q)
	return negativeLockRow(func(dest ...any) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			return errors.New("unbounded context")
		}
		if q == "SELECT current_user" {
			role := "photo_scheduler"
			if t.mode == "role" {
				role = "photo_worker"
			}
			*dest[0].(*string) = role
			return nil
		}
		if q != "SELECT photo.lock_v29_negative_qualification($1,$2,$3,$4,$5::jsonb)" || args[0] != t.tenant {
			return errors.New("wrong query")
		}
		*dest[0].(*bool) = t.mode != "stale"
		return nil
	})
}
func (t *negativeLockTx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	t.calls = append(t.calls, q)
	if q != "SELECT set_config('app.tenant_id',$1,true)" || args[0] != t.tenant.String() {
		return pgconn.CommandTag{}, errors.New("scope")
	}
	return pgconn.CommandTag{}, nil
}
func TestNegativeLiveQualificationLock(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-qualification.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Pin NegativeQualificationPin }
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("fixture")
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	for _, mode := range []string{"ok", "role", "stale"} {
		tx := &negativeLockTx{mode: mode, tenant: tenant}
		err := LockNegativeQualification(context.Background(), tx, who, tenant, project, f.Pin)
		if mode == "ok" && err != nil {
			t.Fatal(err)
		}
		if mode == "role" && (err != v2input.ErrInvalid || len(tx.calls) != 1) {
			t.Fatal(err, tx.calls)
		}
		if mode == "stale" && err != v2input.ErrExpired {
			t.Fatal(err)
		}
		if mode != "role" && len(tx.calls) != 3 {
			t.Fatal(tx.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &negativeLockTx{tenant: tenant}
	if LockNegativeQualification(ctx, tx, who, tenant, project, f.Pin) != context.Canceled || len(tx.calls) != 0 {
		t.Fatal("cancelled gate queried")
	}
}
