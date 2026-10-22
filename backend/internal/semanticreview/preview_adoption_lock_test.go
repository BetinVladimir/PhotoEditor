package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

type shownLockTX struct {
	pgx.Tx
	calls   int
	run     uuid.UUID
	receipt []byte
	err     error
	cancel  context.CancelFunc
}
type shownLockRow struct{ tx *shownLockTX }

func (tx *shownLockTX) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.calls++
	if !strings.Contains(query, "lock_semantic_shown_exposure_params") || len(args) != 13 {
		panic("unexpected query")
	}
	return shownLockRow{tx}
}
func (row shownLockRow) Scan(dest ...any) error {
	tx := row.tx
	if tx.err != nil {
		return tx.err
	}
	*dest[0].(*uuid.UUID) = tx.run
	*dest[1].(*[]byte) = append([]byte(nil), tx.receipt...)
	if tx.cancel != nil {
		tx.cancel()
	}
	return nil
}
func TestShownLockRejectsChangedReceiptAndReturnsNoPartialSelection(t *testing.T) {
	for _, change := range []string{"valid", "run", "fence", "pixels", "extra", "sql", "cancel"} {
		child, draft, selected, physical := shownCompileFixture(t)
		compiled, err := CompileShownExposureAdoption(child, draft, selected, physical)
		if err != nil {
			t.Fatal(err)
		}
		receipt := selected.Receipt
		tx := &shownLockTX{run: selected.PreviewRunID}
		switch change {
		case "run":
			tx.run = uuid.New()
		case "fence":
			receipt.Fence++
		case "pixels":
			receipt.Pixels.ArtifactID = uuid.New()
		case "sql":
			tx.err = errors.New("stale authority")
		}
		tx.receipt, err = canonical.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if change == "extra" {
			tx.receipt = append(tx.receipt[:len(tx.receipt)-1], []byte(`,"forged":true}`)...)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if change == "cancel" {
			tx.cancel = cancel
		}
		got, err := LockShownExposureParams(ctx, tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, physical.linkage.Presentation.Params, compiled)
		if tx.calls != 1 {
			t.Fatal("selection not locked")
		}
		if change == "valid" {
			if err != nil || got != selected {
				t.Fatal("exact selection lost", err)
			}
			continue
		}
		if err == nil || got != (LockedPreviewSelection{}) {
			t.Fatal("invalid selection escaped", change)
		}
	}
}
func TestShownLockRejectsTamperedFrozenParamsBeforeSQL(t *testing.T) {
	child, draft, selected, physical := shownCompileFixture(t)
	compiled, err := CompileShownExposureAdoption(child, draft, selected, physical)
	if err != nil {
		t.Fatal(err)
	}
	compiled.body = append(compiled.body, ' ')
	tx := &shownLockTX{}
	got, err := LockShownExposureParams(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, physical.linkage.Presentation.Params, compiled)
	if err == nil || tx.calls != 0 || got != (LockedPreviewSelection{}) {
		t.Fatal("tampered params reached SQL")
	}
}
