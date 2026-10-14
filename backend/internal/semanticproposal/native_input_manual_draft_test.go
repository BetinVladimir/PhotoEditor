package semanticproposal

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Fingerprint integrity only; synthetic fields are not an admission proof.
func TestNativeManualDraftSealDetectsMutationAndCancellation(t *testing.T) {
	plan := nativeInputManualDraftPlan{}
	var err error
	plan.seal, err = nativeInputManualDraftDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeInputManualDraftSeal(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"config", "dependency", "profile", "costs", "params_version"} {
		changed := plan
		switch field {
		case "config":
			changed.shell.ConfigSHA = "changed"
		case "dependency":
			changed.prepared.stage.dependencies.seal = "changed"
		case "profile":
			changed.prepared.stage.saved.profile = "changed"
		case "costs":
			changed.prepared.stage.saved.recovery.RAM++
		case "params_version":
			changed.prepared.stage.params.object.VersionID = "changed"
		}
		if err := verifyNativeInputManualDraftSeal(context.Background(), changed); err == nil {
			t.Fatal("changed seal accepted", field)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyNativeInputManualDraftSeal(ctx, plan); err != context.Canceled {
		t.Fatal(err)
	}
	recovered, err := (SQLTransferSourceReader{}).restoreNativeInputManualDraft(ctx, nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{})
	if err != context.Canceled || recovered.seal != "" {
		t.Fatal("cancelled recovery exposed plan", err)
	}
	tx := &manualLockTx{role: "photo_scheduler"}
	reader := SQLTransferSourceReader{}
	if _, err := reader.recoverNativeInputManualTransportInTransaction(ctx, tx, auth.Identity{}, plan); err != context.Canceled || len(tx.calls) != 0 {
		t.Fatal("cancelled transport reached SQL", err)
	}
	if _, err := reader.recoverNativeInputManualTransportInTransaction(context.Background(), tx, auth.Identity{}, plan); err == nil || len(tx.calls) != 0 {
		t.Fatal("synthetic sealed transport reached SQL")
	}
	if body, err := reader.marshalNativeInputManualRegistrationBinding(ctx, auth.Identity{}, plan, v2input.Built{}); err != context.Canceled || body != nil {
		t.Fatal("cancelled registration exposed binding", err)
	}
	if body, err := reader.marshalNativeInputManualAdmissionEvidence(ctx, auth.Identity{}, plan, v2input.Built{}); err != context.Canceled || body != nil {
		t.Fatal("cancelled admission exposed packet", err)
	}
	if body, err := reader.marshalNativeInputManualAdmissionEvidence(context.Background(), auth.Identity{}, plan, v2input.Built{}); err == nil || body != nil {
		t.Fatal("synthetic admission exposed packet")
	}
	if _, err := reader.sealNativeInputManualPreparationInTransaction(ctx, tx, auth.Identity{}, plan, v2input.Built{}); err != context.Canceled || len(tx.calls) != 0 {
		t.Fatal("cancelled manual seal reached SQL", err)
	}
	if _, err := reader.sealNativeInputManualPreparationInTransaction(context.Background(), tx, auth.Identity{}, plan, v2input.Built{}); err == nil || len(tx.calls) != 0 {
		t.Fatal("synthetic manual seal reached SQL")
	}
	if _, err := reader.resumeNativeInputManualPreparation(ctx, nil, auth.Identity{}, plan, TransferColorProfile{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := reader.restoreAndResumeNativeInputManualPreparation(ctx, nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := reader.restoreAndResumeNativeInputManualPreparation(context.Background(), nil, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}); err == nil {
		t.Fatal("incomplete originals admitted")
	}
}
