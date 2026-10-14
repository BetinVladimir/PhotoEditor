package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func assertNativeSourceLock(t *testing.T, reader SQLTransferSourceReader, who auth.Identity, tenant uuid.UUID, lock func(context.Context, pgx.Tx) error) {
	t.Helper()
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := lock(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 2 || tx.calls[1] != "SELECT photo.lock_semantic_native_source_authority($1,$2,$3,$4,$5::jsonb)" || len(tx.args[1]) != 5 {
		t.Fatal("missing composed source authority lock")
	}
	args := tx.args[1]
	var packet nativeAdmissionEvidence
	if json.Unmarshal([]byte(args[4].(string)), &packet) != nil || args[0] != tenant || args[1] != reader.ProjectID || args[2] != who.UserID || args[3] != who.SessionID || packet.Tenant != tenant || packet.Actor != who.UserID || packet.Session != who.SessionID {
		t.Fatal("source lock changed retained scope")
	}
	tx = &manualLockTx{role: "photo_api"}
	if err := lock(context.Background(), tx); !errors.Is(err, v2input.ErrInvalid) || len(tx.calls) != 1 {
		t.Fatal("foreign role reached authority lock", err)
	}
	tx = &manualLockTx{role: "photo_scheduler", failAt: 2}
	if err := lock(context.Background(), tx); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("lost false SQL authority", err)
	}
	dependency := errors.New("test SQL unavailable")
	tx = &manualLockTx{role: "photo_scheduler", failAt: 2, err: dependency}
	if err := lock(context.Background(), tx); !errors.Is(err, dependency) {
		t.Fatal("lost SQL error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := lock(ctx, tx); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancelled proof reached SQL", err)
	}
	if err := lock(context.Background(), nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("nil transaction", err)
	}
}
