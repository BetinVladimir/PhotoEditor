package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPreviewAdmissionRejectsInvalidPreparationBeforeIO(t *testing.T) {
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
		got, err := AdmitExposurePreviewRun(ctx, &pgxpool.Pool{}, objects, "fixture",
			auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ExposurePreviewPreparation{}, transform)
		cancel()
		if err == nil || got.RunID != uuid.Nil || got.TaskID != uuid.Nil || got.CommitAttempted || objects.calls != 0 {
			t.Fatal("invalid input escaped", kind, got, err)
		}
		if kind == "cancel" && err != context.Canceled {
			t.Fatal("cancel lost", err)
		}
	}
}

func TestPreviewRecoveryResumesOnlyEmptyDraft(t *testing.T) {
	for _, status := range []string{"queued", "running", "completed", "failed", "cancelled", "cancelling"} {
		state := PreviewInspection{RunID: uuid.New(), TaskID: uuid.New(), RunStatus: status, TaskState: "ready", TaskPresent: true, GraphSealed: true}
		got, resume, err := classifyPreviewRecovery(state)
		if err != nil || resume || !got.Recovered || got.CommitAttempted || got.RunID != state.RunID || got.RunStatus != status {
			t.Fatal("sealed request restarted", status, got, resume, err)
		}
	}
	state := PreviewInspection{RunID: uuid.New(), TaskID: uuid.New(), RunStatus: "draft"}
	if _, resume, err := classifyPreviewRecovery(state); err != nil || !resume {
		t.Fatal("empty draft cannot resume", err)
	}
	state.TaskPresent = true
	state.TaskState = "blocked"
	if _, resume, err := classifyPreviewRecovery(state); err == nil || resume {
		t.Fatal("partial draft silently repaired")
	}
	state.GraphSealed = true
	state.RunStatus = "unknown"
	if _, resume, err := classifyPreviewRecovery(state); err == nil || resume {
		t.Fatal("unknown state accepted")
	}
}
