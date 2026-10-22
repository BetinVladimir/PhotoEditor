package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestBackgroundCleanupPreviewAdmissionRejectsInvalidPreparationBeforeIO(t *testing.T) {
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
		got, err := AdmitBackgroundCleanupPreviewRun(ctx, &pgxpool.Pool{}, objects, "fixture",
			auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, BackgroundCleanupPreviewPreparation{}, transform)
		cancel()
		if err == nil || got.RunID != uuid.Nil || got.TaskID != uuid.Nil || got.CommitAttempted || objects.calls != 0 {
			t.Fatal("invalid input escaped", kind, got, err)
		}
		if kind == "cancel" && err != context.Canceled {
			t.Fatal("cancel lost", err)
		}
	}
}

func TestBackgroundCleanupAdmissionRejectsForeignSourceTargetBeforeRecovery(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, missing := range []bool{false, true} {
		input := BackgroundCleanupPreviewPreparation{}
		input.Target.ID = uuid.New()
		if !missing {
			input.Source.TargetID = uuid.New()
		}
		objects := &forbiddenExposureObjects{}
		// An uninitialized pool deliberately fails if Save/Inspect is reached.
		got, err := AdmitBackgroundCleanupPreviewRun(context.Background(), &pgxpool.Pool{}, objects, "fixture", who, input, []byte("{}"))
		if err != ErrInvalid || got.RunID != uuid.Nil || got.TaskID != uuid.Nil || got.CommitAttempted || objects.calls != 0 {
			t.Fatal("foreign or missing source target escaped admission", missing, got, err)
		}
	}
}
