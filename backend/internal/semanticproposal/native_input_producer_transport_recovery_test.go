package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestNativeProducerTransportRecoveryRefusesBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &manualLockTx{role: "photo_scheduler"}
	for _, testCtx := range []context.Context{ctx, context.Background()} {
		got, err := recoverNativeInputFaceProducerTransportsInTransaction(testCtx, tx, auth.Identity{}, nativeInputRestoredFaceProducers{})
		if err == nil || got.detector.Manifest.ArtifactID != uuid.Nil || got.skin.Manifest.ArtifactID != uuid.Nil || len(tx.calls) != 0 {
			t.Fatal("incomplete producer proof reached SQL")
		}
		if testCtx == ctx && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	got, err := recoverNativeInputFaceProducerTransports(ctx, nil, auth.Identity{}, nativeInputRestoredFaceProducers{})
	if !errors.Is(err, context.Canceled) || got.detector.Manifest.ArtifactID != uuid.Nil {
		t.Fatal("cancelled committed recovery exposed bindings", err)
	}
	deps, err := (SQLTransferSourceReader{}).restoreNativeInputDefectDependencies(ctx, nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}, [2]float64{.002, .012}, nil, nil, nil)
	if !errors.Is(err, context.Canceled) || deps.seal != "" {
		t.Fatal("cancelled dependency restore exposed authority", err)
	}
	plan, err := (SQLTransferSourceReader{}).restoreNativeInputDefectDraftPlan(ctx, nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}, [2]float64{.002, .012}, nil, nil, nil, nil)
	if !errors.Is(err, context.Canceled) || plan.seal != "" {
		t.Fatal("cancelled defect restore exposed graph", err)
	}
}
