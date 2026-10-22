package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestShownOperationRoutingUsesExactSupportedIntent(t *testing.T) {
	exposureCalls, removalCalls := 0, 0
	exposure := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, ApplyDecision) (ShownApplyResolution, error) {
		exposureCalls++
		return ShownApplyResolution{}, nil
	}
	removal := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, ApplyDecision) (ShownApplyResolution, error) {
		removalCalls++
		return ShownApplyResolution{}, nil
	}
	for _, kind := range []string{"local_exposure", "remove_object"} {
		selected, err := selectShownOperationResolver(kind, exposure, removal)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = selected(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, ApplyDecision{}); err != nil {
			t.Fatal(err)
		}
	}
	if exposureCalls != 1 || removalCalls != 1 {
		t.Fatal("wrong resolver selected")
	}
	for _, kind := range []string{"", "semantic.remove_object"} {
		if _, err := selectShownOperationResolver(kind, exposure, removal); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsupported kind %q: %v", kind, err)
		}
	}
	if _, err := selectShownOperationResolver("remove_object", exposure, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing removal fell back to exposure", err)
	}
	if _, err := selectShownOperationResolver("local_exposure", nil, removal); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing exposure fell back to removal", err)
	}
	if exposureCalls != 1 || removalCalls != 1 {
		t.Fatal("rejected selection executed callback")
	}
}
func TestShownOperationResolverRequiresScopedReader(t *testing.T) {
	if _, err := NewSQLShownOperationResolver(nil, nil, 1, "a", "b", nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestShownBackgroundCleanupResolverHasNoFallback(t *testing.T) {
	calls := 0
	cleanup := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, ApplyDecision) (ShownApplyResolution, error) {
		calls++
		return ShownApplyResolution{}, nil
	}
	selected, err := selectShownOperationResolver("background_cleanup", nil, nil, cleanup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = selected(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, ApplyDecision{}); err != nil || calls != 1 {
		t.Fatal("cleanup resolver not selected", err, calls)
	}
	if _, err = selectShownOperationResolver("background_cleanup", cleanup, cleanup); err != ErrUnavailable {
		t.Fatal("cleanup borrowed foreign resolver", err)
	}
	if _, err = selectShownOperationResolver("remove_object", nil, nil, cleanup); err != ErrUnavailable {
		t.Fatal("removal borrowed cleanup", err)
	}
	if _, err = selectShownOperationResolver("background_cleanup", nil, nil, cleanup, cleanup, cleanup); err != ErrInvalid {
		t.Fatal("ambiguous cleanup installation accepted", err)
	}
	if calls != 1 {
		t.Fatal("rejected routing invoked resolver")
	}
}

func TestRetouchShownResolverHasNoFallback(t *testing.T) {
	if _, err := selectShownOperationResolver("retouch_region", nil, nil); err != ErrUnavailable {
		t.Fatal(err)
	}
}
