package semanticproposal

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestNativeManualInitialPreparationRefusesBeforeIO(t *testing.T) {
	reader := SQLTransferSourceReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, testCtx := range []context.Context{ctx, context.Background()} {
		prepared, err := reader.prepareNativeInputManualInitialParams(testCtx, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}, nil, [2]float64{.003, .008})
		if err == nil || prepared.selection.paramsRef != (v2input.Ref{}) || prepared.selection.params.params != "" || prepared.selection.proofs != nil {
			t.Fatal("incomplete initial preparation exposed authority", err)
		}
		if testCtx == ctx && err != context.Canceled {
			t.Fatal(err)
		}
		published, publishErr := reader.publishNativeInputManualInitialParams(testCtx, auth.Identity{}, nativeInputRestoredFaceProducers{}, nativeInputManualInitialPreparation{}, TransferColorProfile{}, v2draft.Publisher{})
		if publishErr == nil || published.selection.paramsRef != (v2input.Ref{}) {
			t.Fatal("incomplete publication exposed accepted ref", publishErr)
		}
		if testCtx == ctx && publishErr != context.Canceled {
			t.Fatal(publishErr)
		}
		proof, proofErr := reader.readNativeInputPublishedManualParams(testCtx, auth.Identity{}, nativeInputRestoredFaceProducers{}, nativeInputPublishedManualParams{}, TransferColorProfile{})
		if proofErr == nil || proof.params.ref != (v2input.Ref{}) || proof.published.selection.paramsRef != (v2input.Ref{}) {
			t.Fatal("incomplete accepted Params proof exposed authority", proofErr)
		}
		if testCtx == ctx && proofErr != context.Canceled {
			t.Fatal(proofErr)
		}
		deps, depsErr := reader.prepareNativeInputManualInitialDependencies(testCtx, nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, nativeInputPublishedManualParams{}, TransferColorProfile{})
		if depsErr == nil || deps.dependencies.seal != "" || deps.params.params.ref != (v2input.Ref{}) {
			t.Fatal("incomplete initial dependencies exposed authority", depsErr)
		}
		if testCtx == ctx && depsErr != context.Canceled {
			t.Fatal(depsErr)
		}
		stage, stageErr := reader.prepareNativeInputManualInitialQualification(testCtx, auth.Identity{}, nativeInputManualInitialDependencies{}, TransferColorProfile{}, v2input.Ref{}, v2input.SemanticQualifiedCosts{})
		if stageErr == nil || stage.qualification.dependencySeal != "" {
			t.Fatal("incomplete initial qualification exposed authority", stageErr)
		}
		if testCtx == ctx && stageErr != context.Canceled {
			t.Fatal(stageErr)
		}
		task, taskErr := reader.compileNativeInputManualInitialTask(testCtx, auth.Identity{}, nativeInputManualInitialStage{}, 1, 0, 1, 1)
		if taskErr == nil || task.compiled.entries != "" || task.compiled.sizes != "" {
			t.Fatal("incomplete initial task exposed preparation", taskErr)
		}
		if testCtx == ctx && taskErr != context.Canceled {
			t.Fatal(taskErr)
		}
		plan, planErr := reader.compileNativeInputManualInitialDraft(testCtx, auth.Identity{}, nativeInputManualInitialTaskPreparation{}, 1)
		if planErr == nil || plan.seal != "" {
			t.Fatal("incomplete initial draft exposed graph", planErr)
		}
		if testCtx == ctx && planErr != context.Canceled {
			t.Fatal(planErr)
		}
		tx := &manualLockTx{role: "photo_scheduler"}
		admission, beginErr := reader.beginNativeInputManualDraftInTransaction(testCtx, tx, auth.Identity{}, nativeInputManualInitialDraftPlan{}, TransferColorProfile{})
		if beginErr == nil || admission.Outcome != "" || len(tx.calls) != 0 {
			t.Fatal("incomplete initial draft reached SQL", beginErr)
		}
		if testCtx == ctx && beginErr != context.Canceled {
			t.Fatal(beginErr)
		}
		transport, _, transportErr := reader.publishNativeInputManualInitialTransport(testCtx, auth.Identity{}, nativeInputManualInitialDraftPlan{}, TransferColorProfile{}, nil)
		if transportErr == nil || transport.Graph.ID != (v2input.Ref{}).ArtifactID {
			t.Fatal("incomplete initial transport exposed graph", transportErr)
		}
		if testCtx == ctx && transportErr != context.Canceled {
			t.Fatal(transportErr)
		}
		packet, packetErr := reader.marshalNativeInputManualInitialAdmissionEvidence(testCtx, auth.Identity{}, nativeInputManualInitialDraftPlan{}, v2input.Built{})
		if packetErr == nil || packet != nil {
			t.Fatal("incomplete initial admission exposed packet", packetErr)
		}
		if testCtx == ctx && packetErr != context.Canceled {
			t.Fatal(packetErr)
		}
		ticket, sealErr := reader.registerAndSealNativeInputManualInitialInTransaction(testCtx, tx, auth.Identity{}, nativeInputManualInitialDraftPlan{}, TransferColorProfile{}, v2input.Built{})
		if sealErr == nil || ticket.State != "" || len(tx.calls) != 0 {
			t.Fatal("incomplete initial seal reached SQL or exposed ticket", sealErr)
		}
		if testCtx == ctx && sealErr != context.Canceled {
			t.Fatal(sealErr)
		}
		registerErr := reader.registerNativeInputManualInitialInputsInTransaction(testCtx, tx, auth.Identity{}, nativeInputManualInitialDraftPlan{}, TransferColorProfile{}, v2input.Built{})
		if registerErr == nil || len(tx.calls) != 0 {
			t.Fatal("incomplete registration reached SQL", registerErr)
		}
		if testCtx == ctx && registerErr != context.Canceled {
			t.Fatal(registerErr)
		}
	}
}
