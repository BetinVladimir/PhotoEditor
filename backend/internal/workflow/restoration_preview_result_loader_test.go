package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

type restorationResultRowsFixture struct{ calls int }

func (f *restorationResultRowsFixture) Read(context.Context, uuid.UUID, uuid.UUID, string, string) ([]MultiPreviewOutput, error) {
	f.calls++
	return nil, nil
}

func TestRestorationPreviewResultLoaderRefusesBeforeObjectIO(t *testing.T) {
	plan, _, before, after := restorationPreviewFixture(t)
	rows := &restorationResultRowsFixture{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadRestorationPreviewResult(ctx, rows, nil, plan, before, after,
		"before", "preview1600", "photos"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := LoadRestorationPreviewResult(context.Background(), rows, nil, plan, before, after,
		"before", "preview1600", "photos"); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal(err)
	}
	if rows.calls != 0 {
		t.Fatal("result observation reached before validation")
	}
}
