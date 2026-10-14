package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPerspectiveLinesPreparationRejectsCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (PerspectiveLinesPreparation{}).Prepare(ctx, PerspectiveLinesPlan{}, uuid.Nil, auth.Identity{})
	if err != context.Canceled {
		t.Fatal(err)
	}
}
func TestPerspectiveLinesPreparationRejectsMissingDependencies(t *testing.T) {
	if _, err := (PerspectiveLinesPreparation{}).Prepare(context.Background(), PerspectiveLinesPlan{}, uuid.Nil, auth.Identity{}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
}
func TestPerspectiveLinesPreparedDraftCannotSealForgedOrCancelledCapability(t *testing.T) {
	if err := (PreparedPerspectiveLinesDraft{}).Seal(context.Background(), nil); err == nil {
		t.Fatal("empty capability accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (PreparedPerspectiveLinesDraft{}).Seal(ctx, nil); err != context.Canceled {
		t.Fatal(err)
	}
}
