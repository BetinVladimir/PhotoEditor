package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestShownPublisherRejectsEmptyFrozenScope(t *testing.T) {
	_, err := NewShownExposurePublisher(nil, nil, "owned", uuid.New(), uuid.New(), uuid.New(), ExposurePreviewParams{}, FrozenShownExposureTask{})
	if err == nil {
		t.Fatal("empty scope admitted")
	}
}

func TestShownPublisherRejectsWrongTaskBeforeIO(t *testing.T) {
	publisher := shownDocumentPublisher{task: uuid.New()}
	if _, err := publisher.Publish(context.Background(), uuid.New(), 0, "Params", []byte("{}")); err == nil {
		t.Fatal("wrong task admitted")
	}
}

func TestShownPublisherHonorsCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	publisher := shownDocumentPublisher{task: uuid.New()}
	if _, err := publisher.Publish(ctx, publisher.task, 0, "Params", []byte("{}")); err != context.Canceled {
		t.Fatalf("error: %v", err)
	}
}
