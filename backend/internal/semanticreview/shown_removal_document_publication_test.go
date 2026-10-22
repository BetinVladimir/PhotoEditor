package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestShownRemovalPublisherRejectsEmptyFrozenScope(t *testing.T) {
	_, err := NewShownRemovalPublisher(nil, nil, "owned", uuid.New(), uuid.New(), uuid.New(), RemovalPreviewParams{}, FrozenShownRemovalTask{})
	if err == nil {
		t.Fatal("empty scope admitted")
	}
}

func TestShownRemovalPublisherRejectsWrongTaskBeforeIO(t *testing.T) {
	publisher := shownRemovalDocumentPublisher{task: uuid.New()}
	if _, err := publisher.Publish(context.Background(), uuid.New(), 0, "Params", []byte("{}")); err == nil {
		t.Fatal("wrong task admitted")
	}
}

func TestShownRemovalPublisherHonorsCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	publisher := shownRemovalDocumentPublisher{task: uuid.New()}
	if _, err := publisher.Publish(ctx, publisher.task, 0, "Params", []byte("{}")); err != context.Canceled {
		t.Fatalf("error: %v", err)
	}
}
