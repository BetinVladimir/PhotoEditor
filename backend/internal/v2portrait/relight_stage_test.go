package v2portrait

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type relightStageNoDB struct{ calls int }

func (d *relightStageNoDB) Begin(context.Context) (pgx.Tx, error) {
	d.calls++
	return nil, errors.New("unexpected database call")
}

func TestRelightStageRejectsChangedFrozenDescriptorBeforeDB(t *testing.T) {
	prepared, ref := relightFrozenFixture(t, 13)
	frozen, err := freezePreparedRelightTask(context.Background(), prepared, ref)
	if err != nil {
		t.Fatal(err)
	}
	db := &relightStageNoDB{}
	coordinator := RelightBootstrapCoordinator{DB: db}
	for _, fault := range []string{"digest", "profile", "entries", "reservation", "owner"} {
		changed := frozen
		switch fault {
		case "digest":
			changed.draft.InputDigest = "changed"
		case "profile":
			changed.draft.ExecutionProfile = []byte(`{"backend":"changed"}`)
		case "entries":
			changed.entries = append([]v2input.Entry(nil), frozen.entries[1:]...)
		case "reservation":
			changed.reservedBytes++
		case "owner":
			changed.draft.ID = ref.ID
		}
		if err = coordinator.StageRelightTask(context.Background(), prepared, changed); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("forged frozen descriptor reached authority", fault, err)
		}
	}
	if db.calls != 0 {
		t.Fatal("forgery reached database")
	}
	raw, err := frozen.Descriptor()
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = '!'
	again, err := frozen.Descriptor()
	if err != nil || again[0] != '{' {
		t.Fatal("borrowed descriptor bytes", err)
	}
}
