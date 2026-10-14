package workflow

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestPerspectivePreviewQuotaRejectsInvalidOrUnavailableScope(t *testing.T) {
	store := PerspectivePreviewIntentStore{}
	if _, err := store.Reserve(context.Background(), PerspectivePreviewPlan{}); err == nil {
		t.Fatal("missing saved plan accepted")
	}
	plan, err := CompilePerspectivePreviewPlan(perspectivePlanBundle(t), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Reserve(context.Background(), plan); err == nil {
		t.Fatal("missing database accepted")
	}
	for _, scope := range [][2]uuid.UUID{{uuid.Nil, uuid.New()}, {uuid.New(), uuid.Nil}, {uuid.New(), uuid.New()}} {
		if _, err = store.Release(context.Background(), scope[0], scope[1]); err == nil {
			t.Fatal("unavailable release accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = store.Reserve(ctx, plan); err != context.Canceled {
		t.Fatal("reserve cancellation lost", err)
	}
	if _, err = store.Release(ctx, uuid.New(), uuid.New()); err != context.Canceled {
		t.Fatal("release cancellation lost", err)
	}
}
