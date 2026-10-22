package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestShownRemovalTaskRejectsMissingFrozenClosureBeforePublication(t *testing.T) {
	recorder := &previewPublicationRecorder{}
	task, err := PublishShownRemovalTask(context.Background(), FrozenShownRemovalTask{}, recorder)
	if err == nil || task.Draft.ID != uuid.Nil || len(recorder.calls) != 0 {
		t.Fatal("empty task published")
	}
}
func TestShownRemovalTaskRejectsPreviewOperationBeforePublication(t *testing.T) {
	var frozen FrozenShownRemovalTask
	frozen.frozen.draft.Operation = "semantic.preview_remove_object"
	recorder := &previewPublicationRecorder{}
	if _, err := PublishShownRemovalTask(context.Background(), frozen, recorder); err == nil || len(recorder.calls) != 0 {
		t.Fatal("wrong operation published")
	}
}
func TestShownRemovalTaskCancelledPublicationDoesNotWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := &previewPublicationRecorder{}
	if _, err := PublishShownRemovalTask(ctx, FrozenShownRemovalTask{}, recorder); err != context.Canceled || len(recorder.calls) != 0 {
		t.Fatal("cancelled publication")
	}
}
