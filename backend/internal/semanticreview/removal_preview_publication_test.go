package semanticreview

import (
	"context"
	"testing"
)

func TestRemovalPublicationRejectsEmptyOrCancelledCompiler(t *testing.T) {
	var frozen FrozenRemovalPreviewTask
	if _, err := frozen.DraftTask(); err == nil {
		t.Fatal("empty compiler projected")
	}
	if _, err := PublishRemovalPreviewTask(context.Background(), frozen, nil); err == nil {
		t.Fatal("empty compiler published")
	}
	if _, err := VerifyRemovalPreviewPublication(context.Background(), frozen, PreparedExposureTask{}); err == nil {
		t.Fatal("unowned result verified")
	}
	if _, err := PrepareRemovalPreviewCommitInputs(context.Background(), frozen, PreparedExposureTask{}); err == nil {
		t.Fatal("empty commit prepared")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishRemovalPreviewTask(ctx, frozen, nil); err != context.Canceled {
		t.Fatal("lost cancellation", err)
	}
}
