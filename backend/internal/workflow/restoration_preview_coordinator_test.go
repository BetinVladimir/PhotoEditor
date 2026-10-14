package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

func TestRestorationPreviewCoordinatorRefusesUninstalledAndCancelled(t *testing.T) {
	coordinator := RestorationPreviewCoordinator{}
	if _, err := coordinator.Prepare(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("uninstalled coordinator accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := coordinator.Prepare(ctx, uuid.New(), uuid.New()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled coordinator accepted", err)
	}
}
