package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestRemovalDocumentPublisherRejectsInvalidScopeBeforeIO(t *testing.T) {
	if _, err := NewRemovalPreviewPublisher(nil, nil, "photos", uuid.New(), uuid.New(), uuid.New(), FrozenRemovalPreviewTask{}); err == nil {
		t.Fatal("empty compiler accepted")
	}
	p := removalPreviewDocumentPublisher{task: uuid.New()}
	if _, err := p.Publish(context.Background(), uuid.New(), 0, "Params", []byte(`{}`)); err == nil {
		t.Fatal("foreign task accepted")
	}
	if _, err := p.Publish(context.Background(), p.task, 0, "Unsupported", []byte(`{}`)); err == nil {
		t.Fatal("unknown contract accepted")
	}
	if _, err := p.Publish(context.Background(), p.task, 1, "Params", []byte(`{}`)); err == nil {
		t.Fatal("foreign params ordinal accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Publish(ctx, p.task, 0, "Params", nil); err != context.Canceled {
		t.Fatal("lost cancellation", err)
	}
}
