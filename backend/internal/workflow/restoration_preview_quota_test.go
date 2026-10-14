package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationPreviewQuotaRequiresInstalledStoreAndExactPlan(t *testing.T) {
	if RestorationPreviewReservedBytes != 2*PerspectivePreviewReservedBytes {
		t.Fatal("four-sided reserve is not twice two-sided reserve")
	}
	plan := restorationPreviewFixturePlan(t)
	store := RestorationPreviewIntentStore{}
	if _, err := store.Reserve(context.Background(), plan); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("uninstalled reserve accepted", err)
	}
	bad := plan
	bad.docs[0].ID = uuid.New()
	if _, err := store.Reserve(context.Background(), bad); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("substituted task accepted", err)
	}
	if _, err := store.Release(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("uninstalled release accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Reserve(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled reserve accepted", err)
	}
}
