package v2draft

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestReflectionDraftBeginFreezesSelectionAndQualification(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha}
	calibration := ref()
	pin := v2portrait.RestorationQualificationPin{Operation: "image.reflection",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: calibration, ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	proposal, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"base": base, "strength": .5, "mask": ref(), "protected": ref(),
		"model_lock_sha": pin.ModelLockSHA, "calibration": calibration})
	if err != nil {
		t.Fatal(err)
	}
	intent := ReflectionClientIntent{SchemaVersion: 1, Snapshot: uuid.New(),
		Ordinal: 7, Proposal: proposal, TargetPixel: ref(), ProtectedPixel: ref()}
	actor := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := ReflectionDraftBeginRequest{Tenant: uuid.New(), Project: uuid.New(),
		Actor: actor, Key: "reflection-begin-1", Intent: intent, Pin: pin,
		ReservedBytes: minRestorationDraftReservation}
	tx := &restorationStoreTx{role: "photo_scheduler", accepted: true}
	beginner := SQLGlareDonorDraftBeginner{DB: restorationStoreDB{tx}}
	got, err := beginner.BeginReflection(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	wantRun, err := v2input.RunID(request.Tenant, actor.UserID, request.Key)
	if err != nil {
		t.Fatal(err)
	}
	wantWorkflow, err := v2input.WorkflowID(wantRun)
	if err != nil {
		t.Fatal(err)
	}
	if got.Run != wantRun || got.Workflow != wantWorkflow ||
		got.BaseVersion != base.AssetVersionID || tx.commits != 1 ||
		len(tx.args) != 4 || tx.args[1] != string(proposal) ||
		tx.args[2] != pin.PolicySHA || tx.args[3] != request.ReservedBytes {
		t.Fatal("reflection draft lost deterministic identity or quota")
	}
	var config struct {
		Intent struct {
			Operation      string      `json:"operation"`
			Run            uuid.UUID   `json:"run_id"`
			Workflow       uuid.UUID   `json:"workflow_id"`
			TargetPixel    v2input.Ref `json:"target_pixel"`
			ProtectedPixel v2input.Ref `json:"protected_pixel"`
			ProposalSHA    string      `json:"params_proposal_sha"`
		} `json:"v28_restoration"`
	}
	if json.Unmarshal([]byte(tx.args[0].(string)), &config) != nil ||
		config.Intent.Operation != "image.reflection" || config.Intent.Run != wantRun ||
		config.Intent.Workflow != wantWorkflow ||
		config.Intent.TargetPixel != intent.TargetPixel ||
		config.Intent.ProtectedPixel != intent.ProtectedPixel {
		t.Fatal("reflection draft configuration lost original manual pair")
	}
	digest, err := canonical.Hash(json.RawMessage(proposal))
	if err != nil || config.Intent.ProposalSHA != digest {
		t.Fatal("reflection proposal digest changed", err)
	}
	request.Intent.ProtectedPixel = request.Intent.TargetPixel
	tx.queries = nil
	if _, err := beginner.BeginReflection(context.Background(), request); err != v2input.ErrInvalid || len(tx.queries) != 0 {
		t.Fatal("aliased reflection pixels reached SQL", err)
	}
}
