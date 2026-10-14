package semanticproposal

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestNativeManualDependenciesRefuseIncompleteProofBeforeSQL(t *testing.T) {
	reader := SQLTransferSourceReader{}
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := reader.lockNativeInputManualDependenciesInTransaction(context.Background(), tx, auth.Identity{}, nativeInputManualDependencies{}); err == nil || len(tx.calls) != 0 {
		t.Fatal("empty dependency proof reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reader.lockNativeInputManualDependenciesInTransaction(ctx, tx, auth.Identity{}, nativeInputManualDependencies{}); err != context.Canceled || len(tx.calls) != 0 {
		t.Fatal("cancelled dependency proof reached SQL", err)
	}
	proof, err := reader.prepareNativeInputManualDependencies(ctx, auth.Identity{}, nativeInputRestoredFaceProducers{}, nativeInputManualSelection{}, TransferColorProfile{}, nativeInputFaceProducerTransports{})
	if err != context.Canceled || proof.seal != "" {
		t.Fatal("cancelled dependency preparation exposed proof", err)
	}
	proof, err = reader.restoreNativeInputManualDependencies(ctx, nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{})
	if err != context.Canceled || proof.seal != "" {
		t.Fatal("cancelled original dependency recovery exposed proof", err)
	}
	proof, err = reader.restoreNativeInputManualDependencies(context.Background(), nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{})
	if err == nil || proof.seal != "" {
		t.Fatal("incomplete original dependency recovery exposed proof")
	}
	qualified, err := reader.prepareNativeInputManualStageQualification(ctx, auth.Identity{}, nativeInputManualDependencies{}, TransferColorProfile{}, v2input.Ref{}, v2input.SemanticQualifiedCosts{})
	if err != context.Canceled || qualified.dependencySeal != "" {
		t.Fatal("cancelled qualification exposed proof", err)
	}
	if err := reader.lockNativeInputManualStageInTransaction(ctx, tx, auth.Identity{}, nativeInputManualDependencies{}, nativeInputManualStageQualification{}); err != context.Canceled || len(tx.calls) != 0 {
		t.Fatal("cancelled stage reached SQL", err)
	}
	stage, err := reader.restoreNativeInputQualifiedManualStage(ctx, nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{})
	if err != context.Canceled || stage.dependencies.seal != "" || stage.qualification.dependencySeal != "" {
		t.Fatal("cancelled original stage recovery exposed authority", err)
	}
	entries, sizes, err := reader.compileNativeInputManualInventory(ctx, auth.Identity{}, nativeInputManualDependencies{}, defectEvidenceObject{})
	if err != context.Canceled || entries != nil || sizes != nil {
		t.Fatal("cancelled original inventory exposed files", err)
	}
	entries, sizes, err = reader.compileNativeInputManualInventory(context.Background(), auth.Identity{}, nativeInputManualDependencies{}, defectEvidenceObject{})
	if err == nil || entries != nil || sizes != nil {
		t.Fatal("incomplete original inventory exposed files")
	}
	task, err := reader.compileNativeInputRestoredManualTask(ctx, auth.Identity{}, nativeInputRestoredManualStage{})
	if err != context.Canceled || task.entries != "" || task.sizes != "" {
		t.Fatal("cancelled original task exposed preparation", err)
	}
	task, err = reader.compileNativeInputRestoredManualTask(context.Background(), auth.Identity{}, nativeInputRestoredManualStage{})
	if err == nil || task.entries != "" || task.sizes != "" {
		t.Fatal("incomplete original task exposed preparation")
	}
}
