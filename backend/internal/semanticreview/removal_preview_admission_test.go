package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRemovalPreviewAdmissionRejectsInvalidPreparationBeforeIO(t *testing.T) {
	for _, kind := range []string{"cancel", "transform", "review"} {
		ctx, cancel := context.WithCancel(context.Background())
		transform := []byte("{}")
		if kind == "cancel" {
			cancel()
		}
		if kind == "transform" {
			transform = nil
		}
		objects := &forbiddenExposureObjects{}
		got, err := AdmitRemovalPreviewRun(ctx, &pgxpool.Pool{}, objects, "fixture",
			auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, RemovalPreviewPreparation{}, transform)
		cancel()
		if err == nil || got.RunID != uuid.Nil || got.TaskID != uuid.Nil || got.CommitAttempted || objects.calls != 0 {
			t.Fatal("invalid input escaped", kind, got, err)
		}
		if kind == "cancel" && err != context.Canceled {
			t.Fatal("cancel lost", err)
		}
	}
}
