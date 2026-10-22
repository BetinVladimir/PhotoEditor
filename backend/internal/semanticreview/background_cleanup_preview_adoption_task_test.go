package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestShownBackgroundCleanupTaskRejectsMissingFrozenClosureBeforePublication(t *testing.T) {
	recorder := &previewPublicationRecorder{}
	task, err := PublishShownBackgroundCleanupTask(context.Background(), FrozenShownBackgroundCleanupTask{}, recorder)
	if err == nil || task.Draft.ID != uuid.Nil || len(recorder.calls) != 0 {
		t.Fatal("empty task published")
	}
}
func TestShownBackgroundCleanupTaskRejectsPreviewOperationBeforePublication(t *testing.T) {
	var frozen FrozenShownBackgroundCleanupTask
	frozen.frozen.draft.Operation = "semantic.preview_background_cleanup"
	recorder := &previewPublicationRecorder{}
	if _, err := PublishShownBackgroundCleanupTask(context.Background(), frozen, recorder); err == nil || len(recorder.calls) != 0 {
		t.Fatal("wrong operation published")
	}
}
func TestShownBackgroundCleanupTaskCancelledPublicationDoesNotWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := &previewPublicationRecorder{}
	if _, err := PublishShownBackgroundCleanupTask(ctx, FrozenShownBackgroundCleanupTask{}, recorder); err != context.Canceled || len(recorder.calls) != 0 {
		t.Fatal("cancelled publication")
	}
}
