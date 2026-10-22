package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestShownTaskRejectsMissingFrozenClosureBeforePublication(t *testing.T) {
	recorder := &previewPublicationRecorder{}
	task, err := PublishShownExposureTask(context.Background(), FrozenShownExposureTask{}, recorder)
	if err == nil || task.Draft.ID != uuid.Nil || len(recorder.calls) != 0 {
		t.Fatal("empty task published")
	}
}
func TestShownTaskRejectsPreviewOperationBeforePublication(t *testing.T) {
	var frozen FrozenShownExposureTask
	frozen.frozen.draft.Operation = "semantic.preview_local_exposure"
	recorder := &previewPublicationRecorder{}
	if _, err := PublishShownExposureTask(context.Background(), frozen, recorder); err == nil || len(recorder.calls) != 0 {
		t.Fatal("wrong operation published")
	}
}
func TestShownTaskCancelledPublicationDoesNotWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := &previewPublicationRecorder{}
	if _, err := PublishShownExposureTask(ctx, FrozenShownExposureTask{}, recorder); err != context.Canceled || len(recorder.calls) != 0 {
		t.Fatal("cancelled publication")
	}
}
