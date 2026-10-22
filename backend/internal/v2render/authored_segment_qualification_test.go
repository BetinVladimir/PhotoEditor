package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredSegmentQualificationCancellationPrecedesPool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (SQLAuthoredSegmentQualificationResolver{}).Resolve(ctx, auth.Identity{}, uuid.Nil, uuid.Nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
func TestAuthoredSegmentQualificationRefusesMissingPool(t *testing.T) {
	_, err := (SQLAuthoredSegmentQualificationResolver{}).Resolve(context.Background(), auth.Identity{}, uuid.New(), uuid.New())
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("expected invalid authority, got %v", err)
	}
}
func TestAuthoredSegmentLoaderRefusesMissingDependencies(t *testing.T) {
	_, err := LoadCalibratedAuthoredSegmentRequest(context.Background(), nil, nil, "", nil, uuid.New(), uuid.New(), auth.Identity{}, RenderSelectionItem{})
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("expected invalid dependencies, got %v", err)
	}
}
