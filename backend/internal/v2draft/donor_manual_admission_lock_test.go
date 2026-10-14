package v2draft

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type donorManualLockTx struct {
	pgx.Tx
	origin DonorManualAdmissionBinding
	mode   string
	calls  int
	args   []any
}
type donorManualLockRow struct {
	tx   *donorManualLockTx
	role bool
}

func (t *donorManualLockTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	t.calls++
	if q == "SELECT current_user" {
		return donorManualLockRow{t, true}
	}
	t.args = append([]any(nil), args...)
	return donorManualLockRow{t, false}
}
func (r donorManualLockRow) Scan(dest ...any) error {
	if r.role {
		role := "photo_scheduler"
		if r.tx.mode == "role" {
			role = "photo_api"
		}
		*(dest[0].(*string)) = role
		return nil
	}
	if r.tx.mode == "revoked" {
		return pgx.ErrNoRows
	}
	if r.tx.mode == "db" {
		return errors.New("database")
	}
	*(dest[0].(*uuid.UUID)) = r.tx.origin.root.ArtifactID
	*(dest[1].(*string)) = r.tx.origin.root.SHA256
	*(dest[2].(*uuid.UUID)) = r.tx.origin.original.ArtifactID
	*(dest[3].(*string)) = r.tx.origin.original.SHA256
	if r.tx.mode == "changed" {
		*(dest[0].(*uuid.UUID)) = uuid.New()
	}
	return nil
}
func checkDonorManualLock(t *testing.T, loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence, origin DonorManualAdmissionBinding) {
	t.Helper()
	for _, mode := range []string{"ok", "role", "revoked", "changed", "db", "cancelled"} {
		tx := &donorManualLockTx{origin: origin, mode: mode}
		ctx, cancel := context.WithCancel(context.Background())
		if mode == "cancelled" {
			cancel()
		}
		err := LockDonorManualAdmissionBinding(ctx, tx, uuid.New(), uuid.New(), uuid.New(), loaded, prepared)
		cancel()
		if (err == nil) != (mode == "ok") {
			t.Fatal("manual lock", mode, err)
		}
		if mode == "role" && tx.calls != 1 {
			t.Fatal("wrong role queried origin")
		}
		if mode == "cancelled" && tx.calls != 0 {
			t.Fatal("cancelled lock queried database")
		}
		if mode == "ok" {
			if len(tx.args) != 9 || tx.args[3] != origin.targetPixel.ArtifactID || tx.args[4] != origin.targetPixel.SHA256 || tx.args[5] != origin.guardPixel.ArtifactID || tx.args[6] != origin.guardPixel.SHA256 {
				t.Fatal("changed pixel scope")
			}
			if origin.vectors == nil {
				if tx.args[7] != nil || tx.args[8] != nil {
					t.Fatal("inline vectors expanded")
				}
			} else if tx.args[7] != origin.vectors.ArtifactID || tx.args[8] != origin.vectors.SHA256 {
				t.Fatal("changed vectors")
			}
		}
		if mode == "revoked" || mode == "changed" {
			if !errors.Is(err, v2input.ErrExpired) {
				t.Fatal("origin rejection not expired", err)
			}
		}
	}
}
