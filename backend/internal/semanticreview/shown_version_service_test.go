package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestShownVersionServiceRejectsIncompleteInstallation(t *testing.T) {
	if _, err := NewShownVersionService(nil, nil, "photos"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error=%v", err)
	}
	var service *ShownVersionService
	if _, err := service.Materialize(context.Background(), auth.Identity{}, uuid.New(), uuid.New()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
}
func TestShownVersionServiceCancellationPrecedesDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var service *ShownVersionService
	if _, err := service.Materialize(ctx, auth.Identity{}, uuid.New(), uuid.New()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
