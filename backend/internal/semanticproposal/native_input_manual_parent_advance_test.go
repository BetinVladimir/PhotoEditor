package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestManualParentAdvanceRefusesMissingAnchorBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := (TransferDraftPreparer{}).advanceNativeManualParent(current, nil, auth.Identity{}, nativePreparationAnchor{})
		if err == nil || out.phase != "" || out.ticket != (v2input.JobTicket{}) {
			t.Fatal("incomplete continuation exposed progress", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
