package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestInitialPersonPreparationCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (nativePreparationBootstrap{}).ensureInitialPersonPreparation(ctx, auth.Identity{}, nativePreparationAnchor{}, TransferPreparation{}, 1, nil); err != context.Canceled {
		t.Fatal("cancelled person orchestration reached IO", err)
	}
	if err := (nativePreparationBootstrap{}).ensureInitialPersonPreparation(context.Background(), auth.Identity{}, nativePreparationAnchor{}, TransferPreparation{}, 1, nil); err == nil {
		t.Fatal("missing person dependencies accepted")
	}
}
