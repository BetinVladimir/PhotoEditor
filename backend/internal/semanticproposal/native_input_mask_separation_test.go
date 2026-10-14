package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestNativeInputSelectionSeparationCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	count, proof, err := (SQLTransferSourceReader{}).verifyNativeInputSelectionSeparation(ctx, auth.Identity{}, Request{}, nativeInputSourceContinuation{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || count != 0 || proof.count != 0 {
		t.Fatalf("cancelled request returned authority: count=%d err=%v", count, err)
	}
}
