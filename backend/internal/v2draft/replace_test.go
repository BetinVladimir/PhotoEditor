package v2draft

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

func TestReplaceRevisionAndStableDocumentIDs(t *testing.T) {
	for _, raw := range []string{"", "00", "01", "-1", "+1", "1.0", "9223372036854775808"} {
		if _, err := replaceRevision(raw); err == nil {
			t.Fatalf("accepted revision %q", raw)
		}
	}
	for _, raw := range []string{"0", "1", "9223372036854775807"} {
		if _, err := replaceRevision(raw); err != nil {
			t.Fatalf("rejected revision %q: %v", raw, err)
		}
	}
	tenant, actor, asset := uuid.New(), uuid.New(), uuid.New()
	recipe := stableDraftID(tenant, actor, asset, "retry-key", "recipe")
	if recipe == uuid.Nil || recipe != stableDraftID(tenant, actor, asset, "retry-key", "recipe") ||
		recipe == stableDraftID(tenant, actor, asset, "retry-key", "base") ||
		recipe == stableDraftID(tenant, actor, uuid.New(), "retry-key", "recipe") {
		t.Fatal("draft document ID is not retry-stable and scope-specific")
	}
}

func TestDraftMutationInvalidReferenceIsClientError(t *testing.T) {
	err := draftMutationError(v2input.ErrInvalid)
	var got *domain.Error
	if !errors.As(err, &got) || got.HTTPStatus != 422 {
		t.Fatalf("invalid closure should be 422, got %v", err)
	}
}
