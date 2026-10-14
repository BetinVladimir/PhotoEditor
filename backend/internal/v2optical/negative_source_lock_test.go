package v2optical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNegativeSourceInvalidBytesBeforeSQL(t *testing.T) {
	params, err := os.ReadFile("testdata/negative-params.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	tx := &negativeLockTx{tenant: uuid.New()}
	source = append(source, byte(' '))
	err = LockNegativeSource(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tx.tenant, uuid.New(), uuid.New(), params, source)
	if err == nil || len(tx.calls) != 0 {
		t.Fatal("invalid source reached SQL", err, tx.calls)
	}
}

type negativeSourceScopeTx struct{ negativeLockTx }

func (t *negativeSourceScopeTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	t.calls = append(t.calls, q)
	return negativeLockRow(func(dest ...any) error {
		if q == "SELECT current_user" {
			*dest[0].(*string) = "photo_scheduler"
			return nil
		}
		if q == "SELECT photo.lock_v29_negative_qualification($1,$2,$3,$4,$5::jsonb)" || q == "SELECT photo.lock_v29_negative_qualification_objects($1,$2,$3,$4,$5::jsonb,$6::jsonb)" {
			*dest[0].(*bool) = t.mode != "qualification_stale"
			return nil
		}
		if q == "SELECT photo.lock_v29_negative_source_child($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)" {
			if len(args) != 10 || args[0] != t.tenant {
				return fmt.Errorf("wrong child scope")
			}
			*dest[0].(*bool) = t.mode != "child_stale"
			return nil
		}
		if q != "SELECT photo.v28_local_portrait_source_scope($1,$2,$3,$4,$5,$6::jsonb)" || len(args) != 6 || args[0] != t.tenant {
			return fmt.Errorf("wrong source scope")
		}
		*dest[0].(*bool) = t.mode != "stale"
		return nil
	})
}
func TestNegativeCurrentSourceScope(t *testing.T) {
	params, _ := os.ReadFile("testdata/negative-params.json")
	source, _ := os.ReadFile("testdata/negative-source.json")
	parsed, err := ParseNegativeParams(params)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	parsed.Base.Source.SHA256 = hex.EncodeToString(sum[:])
	parsed.Base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	params, _, err = parsed.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ok", "stale"} {
		tx := &negativeSourceScopeTx{negativeLockTx: negativeLockTx{mode: mode, tenant: uuid.New()}}
		err := LockNegativeSource(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tx.tenant, uuid.New(), uuid.New(), params, source)
		if mode == "ok" && err != nil {
			t.Fatal(err)
		}
		if mode == "stale" && err != v2input.ErrExpired {
			t.Fatal(err)
		}
		if len(tx.calls) != 3 {
			t.Fatal(tx.calls)
		}
	}
}

func TestNegativeSourceInputEdgesChecked(t *testing.T) {
	raw, _ := os.ReadFile("testdata/negative-params.json")
	source, _ := os.ReadFile("testdata/negative-source.json")
	parsed, err := ParseNegativeParams(raw)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	parsed.Base.Source.SHA256 = hex.EncodeToString(sum[:])
	parsed.Base.CanvasHash, _ = canonical.Hash(image.Canvas)
	raw, _, err = parsed.Compile()
	if err != nil {
		t.Fatal(err)
	}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	_, edges, err := NegativeImageInputs(parsed, source, report)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ok", "child_stale"} {
		tx := &negativeSourceScopeTx{negativeLockTx: negativeLockTx{mode: mode, tenant: uuid.New()}}
		err = LockNegativeSourceInputs(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tx.tenant, uuid.New(), uuid.New(), raw, source, report)
		if mode == "ok" && (err != nil || len(tx.calls) != 3+len(edges)) {
			t.Fatal(err, tx.calls)
		}
		if mode == "child_stale" && (err != v2input.ErrExpired || len(tx.calls) != 4) {
			t.Fatal(err, tx.calls)
		}
	}
}

func TestNegativeSourceInputsPreflightBeforeSQL(t *testing.T) {
	for _, mode := range []string{"cancelled", "oversized"} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if mode == "cancelled" {
			cancel()
		}
		tx := &negativeLockTx{tenant: uuid.New()}
		err := LockNegativeSourceInputs(ctx, tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tx.tenant, uuid.New(), uuid.New(), make([]byte, 65537), []byte("{}"), v2input.Ref{})
		if err == nil || len(tx.calls) != 0 {
			t.Fatal("preflight reached SQL", err, tx.calls)
		}
	}
}
