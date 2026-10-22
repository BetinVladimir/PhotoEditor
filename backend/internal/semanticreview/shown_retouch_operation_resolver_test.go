package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestShownRetouchResolverHasNoFallback(t *testing.T) {
	calls := 0
	other := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, ApplyDecision) (ShownApplyResolution, error) {
		t.Fatal("wrong family called")
		return ShownApplyResolution{}, ErrInvalid
	}
	own := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, ApplyDecision) (ShownApplyResolution, error) {
		calls++
		return ShownApplyResolution{}, nil
	}
	if _, err := selectShownOperationResolver("retouch_region", other, other, other); err != ErrUnavailable {
		t.Fatal(err)
	}
	selected, err := selectShownOperationResolver("retouch_region", other, other, other, own)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = selected(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), ApplyDecision{}); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if _, err = selectShownOperationResolver("retouch_region", other, other, other, own, own); err != ErrInvalid {
		t.Fatal(err)
	}
}
