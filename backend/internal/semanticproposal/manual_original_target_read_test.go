package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestOriginalManualTargetReadCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mask, err := (SQLTransferSourceReader{}).readOriginalManualTarget(ctx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, v2input.Ref{})
	if !errors.Is(err, context.Canceled) || mask.Artifact.ID != uuid.Nil {
		t.Fatal("cancelled locator exposed manual target", err)
	}
}
