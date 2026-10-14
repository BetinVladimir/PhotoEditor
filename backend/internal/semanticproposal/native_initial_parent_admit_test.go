package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestInitialDefectParentRejectsIncompleteAuthorityBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ticket, err := (TransferDraftPreparer{}).admitInitialPersonDefectParent(ctx, auth.Identity{}, nativeInitialDefectAdvance{})
	if !errors.Is(err, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled parent exposed receipt", err)
	}
	for _, phase := range []nativeInputPreparationPhase{"", nativePreparationBlocked, nativePreparationWaiting, nativePreparationFailed, nativePreparationSucceeded} {
		ticket, err := (TransferDraftPreparer{}).admitInitialPersonDefectParent(context.Background(), auth.Identity{}, nativeInitialDefectAdvance{phase: phase})
		if err == nil || ticket != (v2input.JobTicket{}) {
			t.Fatal("incomplete protection admitted parent", phase, err)
		}
	}
}
