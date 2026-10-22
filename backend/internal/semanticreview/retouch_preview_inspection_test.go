package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRetouchInspectionCannotTreatUnavailableStateAsRollback(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	got, err := InspectRetouchPreviewRun(context.Background(), nil, who, uuid.New(), retouchParamsFixture(), v2input.Ref{})
	if err != ErrInvalid || got.RunID != uuid.Nil || got.TaskPresent {
		t.Fatal("unavailable inspection fabricated recovery", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = InspectRetouchPreviewRun(ctx, nil, who, uuid.New(), retouchParamsFixture(), v2input.Ref{})
	if err != context.Canceled || got.RunID != uuid.Nil {
		t.Fatal("cancelled inspection fabricated state", got, err)
	}
}
