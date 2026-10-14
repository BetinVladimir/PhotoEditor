package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/auth"
)

func TestNativeManualLocksRejectIncompleteScopeBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &manualLockTx{role: "photo_scheduler"}
	reader := SQLTransferSourceReader{}
	for _, testCtx := range []context.Context{ctx, context.Background()} {
		err := reader.lockNativeInputManualSelectionInTransaction(testCtx, tx, auth.Identity{}, nativeInputRestoredFaceProducers{}, nativeInputManualSelection{})
		if err == nil || len(tx.calls) != 0 {
			t.Fatal("incomplete native manual scope reached SQL")
		}
		if testCtx == ctx && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}
