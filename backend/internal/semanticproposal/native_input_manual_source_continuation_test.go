package semanticproposal

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
)

func TestManualSourceContinuationRefusesIncompleteProtectionBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := composeNativeInputManualSourceContinuation(current, auth.Identity{}, Request{}, sourcePreparationProof{}, registeredNativeInputManualInitialProtection{}, TransferColorProfile{})
		if err == nil || out.protection.verified.Body != nil || out.source.bucket != "" {
			t.Fatal("incomplete protection exposed continuation", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
		tx := &manualLockTx{role: "photo_scheduler"}
		err = (SQLTransferSourceReader{}).lockNativeInputManualSourceContinuation(current, tx, auth.Identity{}, Request{}, nativeInputManualSourceContinuation{}, TransferColorProfile{}, nil)
		if err == nil || len(tx.calls) != 0 {
			t.Fatal("incomplete manual source continuation reached SQL", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
