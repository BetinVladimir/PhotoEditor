package provenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
)

func TestDelegatedRenderInputBytesRequiresScopedCatalog(t *testing.T) {
	if err := VerifyDelegatedRenderInputBytes(context.Background(), nil, nil,
		uuid.New(), v2draft.TaskInputEvidence{}, ""); err == nil {
		t.Fatal("unconfigured physical verifier accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyDelegatedRenderInputBytes(ctx, nil, nil,
		uuid.New(), v2draft.TaskInputEvidence{}, ""); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
