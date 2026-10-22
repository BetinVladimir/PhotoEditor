package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestShownRetouchPublisherRejectsEmptyFrozenScope(t *testing.T) {
	_, err := NewShownRetouchPublisher(nil, nil, "owned", uuid.New(), uuid.New(), uuid.New(), RetouchPreviewParams{}, FrozenShownRetouchTask{})
	if err == nil {
		t.Fatal("empty scope admitted")
	}
}

func TestShownRetouchPublisherRejectsWrongTaskBeforeIO(t *testing.T) {
	publisher := shownRetouchDocumentPublisher{task: uuid.New()}
	if _, err := publisher.Publish(context.Background(), uuid.New(), 0, "Params", []byte("{}")); err == nil {
		t.Fatal("wrong task admitted")
	}
}

func TestShownRetouchPublisherHonorsCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	publisher := shownRetouchDocumentPublisher{task: uuid.New()}
	if _, err := publisher.Publish(ctx, publisher.task, 0, "Params", []byte("{}")); err != context.Canceled {
		t.Fatalf("error: %v", err)
	}
}
