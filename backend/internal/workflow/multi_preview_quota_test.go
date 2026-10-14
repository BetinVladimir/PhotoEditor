package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiPreviewQuotaRefusesInvalidOrUnconfiguredStore(t *testing.T) {
	store := MultiPreviewIntentStore{}
	if ok, err := store.Reserve(context.Background(), MultiPreviewPlan{}); ok || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("zero plan: %v %v", ok, err)
	}
	if ok, err := store.Release(context.Background(), uuid.New(), uuid.New()); ok || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("unconfigured release: %v %v", ok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := store.Reserve(ctx, MultiPreviewPlan{}); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel reserve: %v %v", ok, err)
	}
	if ok, err := store.Release(ctx, uuid.Nil, uuid.Nil); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel release: %v %v", ok, err)
	}
}
