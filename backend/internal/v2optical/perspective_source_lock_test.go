package v2optical

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type perspectiveScopeTx struct{ negativeLockTx }

func (t *perspectiveScopeTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	t.calls = append(t.calls, q)
	return negativeLockRow(func(dest ...any) error {
		if q == "SELECT current_user" {
			*dest[0].(*string) = "photo_scheduler"
			return nil
		}
		if q == "SELECT photo.v28_local_portrait_source_scope($1,$2,$3,$4,$5,$6::jsonb)" {
			if len(args) != 6 || args[0] != t.tenant {
				return fmt.Errorf("wrong source scope")
			}
			*dest[0].(*bool) = t.mode != "stale"
			return nil
		}
		if q == "SELECT photo.lock_v29_perspective_source_child($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)" {
			if len(args) != 10 || args[0] != t.tenant {
				return fmt.Errorf("wrong child scope")
			}
			*dest[0].(*bool) = t.mode != "child_stale"
			return nil
		}
		return fmt.Errorf("unexpected SQL")
	})
}
func TestPerspectiveSourceOccurrenceAuthority(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	p := perspectiveIntent()
	p.Base.Source.SHA256, err = canonical.Hash(image)
	if err != nil {
		t.Fatal(err)
	}
	p.Base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	params := mustPerspectiveParams(t, p)
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Base.CanvasHash}
	calibration := v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Base.CanvasHash}
	for _, mode := range []string{"ok", "stale", "child_stale", "bad_bytes", "cancelled"} {
		tx := &perspectiveScopeTx{negativeLockTx: negativeLockTx{mode: mode, tenant: uuid.New()}}
		source := append([]byte(nil), raw...)
		if mode == "bad_bytes" {
			source = append(source, 32)
		}
		ctx := context.Background()
		if mode == "cancelled" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		err := LockPerspectiveSourceInputs(ctx, tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tx.tenant, uuid.New(), uuid.New(), params, source, report, calibration)
		if mode == "ok" && err != nil {
			t.Fatal(err)
		}
		if mode != "ok" && err == nil {
			t.Fatal("invalid authority accepted", mode)
		}
		if (mode == "bad_bytes" || mode == "cancelled") && len(tx.calls) != 0 {
			t.Fatal("invalid input reached SQL", tx.calls)
		}
	}
}
