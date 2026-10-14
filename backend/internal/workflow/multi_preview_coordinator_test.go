package workflow

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestMultiPreviewCoordinatorPreflight(t *testing.T) {
	c := MultiPreviewCoordinator{}
	for _, operation := range []string{"optical.panorama", "optical.focus_stack", "media.preview"} {
		if _, err := c.Prepare(context.Background(), uuid.New(), uuid.New(), operation); err == nil {
			t.Fatal("unconfigured preparation accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Prepare(ctx, uuid.New(), uuid.New(), "optical.panorama"); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
