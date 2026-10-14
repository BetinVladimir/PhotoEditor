package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestInitialPersonDefectCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := (nativePreparationBootstrap{}).ensureInitialPersonDefect(ctx, auth.Identity{}, nativeInputRestoredFaceProducers{}, nativePersonInputAuthority{}, TransferPreparation{}, 1, nil)
	if !errors.Is(err, context.Canceled) || out.plan.seal != "" || out.ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled automatic defect exposed admission", err)
	}
	out, err = (nativePreparationBootstrap{}).ensureInitialPersonDefect(context.Background(), auth.Identity{}, nativeInputRestoredFaceProducers{}, nativePersonInputAuthority{}, TransferPreparation{}, 1, nil)
	if err == nil || out.plan.seal != "" || out.ticket != (v2input.JobTicket{}) {
		t.Fatal("incomplete automatic defect exposed admission", err)
	}
}
