package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestShownBackgroundCleanupPublisherRejectsEmptyFrozenScope(t *testing.T) {
	_, err := NewShownBackgroundCleanupPublisher(nil, nil, "owned", uuid.New(), uuid.New(), uuid.New(), BackgroundCleanupPreviewParams{}, FrozenShownBackgroundCleanupTask{})
	if err == nil {
		t.Fatal("empty scope admitted")
	}
}

func TestShownBackgroundCleanupPublisherRejectsWrongTaskBeforeIO(t *testing.T) {
	publisher := shownBackgroundCleanupDocumentPublisher{task: uuid.New()}
	if _, err := publisher.Publish(context.Background(), uuid.New(), 0, "Params", []byte("{}")); err == nil {
		t.Fatal("wrong task admitted")
	}
}

func TestShownBackgroundCleanupPublisherHonorsCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	publisher := shownBackgroundCleanupDocumentPublisher{task: uuid.New()}
	if _, err := publisher.Publish(ctx, publisher.task, 0, "Params", []byte("{}")); err != context.Canceled {
		t.Fatalf("error: %v", err)
	}
}
