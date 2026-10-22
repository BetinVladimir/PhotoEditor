package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestShownRetouchTaskRejectsMissingFrozenClosureBeforePublication(t *testing.T) {
	recorder := &previewPublicationRecorder{}
	task, err := PublishShownRetouchTask(context.Background(), FrozenShownRetouchTask{}, recorder)
	if err == nil || task.Draft.ID != uuid.Nil || len(recorder.calls) != 0 {
		t.Fatal("empty task published")
	}
}
func TestShownRetouchTaskRejectsPreviewOperationBeforePublication(t *testing.T) {
	var frozen FrozenShownRetouchTask
	frozen.frozen.draft.Operation = "semantic.preview_retouch_region"
	recorder := &previewPublicationRecorder{}
	if _, err := PublishShownRetouchTask(context.Background(), frozen, recorder); err == nil || len(recorder.calls) != 0 {
		t.Fatal("wrong operation published")
	}
}
func TestShownRetouchTaskCancelledPublicationDoesNotWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := &previewPublicationRecorder{}
	if _, err := PublishShownRetouchTask(ctx, FrozenShownRetouchTask{}, recorder); err != context.Canceled || len(recorder.calls) != 0 {
		t.Fatal("cancelled publication")
	}
}
