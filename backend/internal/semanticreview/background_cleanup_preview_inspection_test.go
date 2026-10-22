package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestBackgroundCleanupInspectionCannotTreatUnavailableStateAsRollback(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	got, err := InspectBackgroundCleanupPreviewRun(context.Background(), nil, who, uuid.New(), backgroundCleanupParamsFixture())
	if err != ErrInvalid || got.RunID != uuid.Nil || got.TaskPresent {
		t.Fatal("unavailable inspection fabricated recovery", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = InspectBackgroundCleanupPreviewRun(ctx, nil, who, uuid.New(), backgroundCleanupParamsFixture())
	if err != context.Canceled || got.RunID != uuid.Nil {
		t.Fatal("cancelled inspection fabricated state", got, err)
	}
}
