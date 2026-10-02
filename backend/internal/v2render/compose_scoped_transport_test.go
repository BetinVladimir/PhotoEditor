package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCompositionScopedTransportRequiresIdentityAndDependency(t *testing.T) {
	publisher := CompositionTransportPublisher{}
	if _, err := publisher.Publish(context.Background(), uuid.New(), uuid.New(), 0, FrozenCompositionInputs{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := publisher.Publish(ctx, uuid.New(), uuid.New(), 0, FrozenCompositionInputs{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
