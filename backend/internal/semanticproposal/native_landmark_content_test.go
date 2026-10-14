package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestNativeLandmarkContentCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := (SQLTransferSourceReader{}).readCompletedNativeLandmarks(ctx, auth.Identity{}, nativeFaceDraftPlan{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || got.catalog.ResultID != uuid.Nil || got.body != "" || got.paramsBody != "" || got.faces != nil {
		t.Fatal("canceled content lookup exposed proof", err)
	}
}
