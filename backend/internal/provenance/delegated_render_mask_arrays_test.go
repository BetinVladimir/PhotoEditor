package provenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
)

func TestDelegatedRenderMaskRefArraysHonorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyDelegatedRenderMaskRefArrays(ctx, nil, nil, uuid.New(),
		v2draft.TaskInputEvidence{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
