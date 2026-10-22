package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestBackgroundCleanupPublicationRejectsEmptyOrCancelledCompiler(t *testing.T) {
	var frozen FrozenBackgroundCleanupPreviewTask
	if _, err := frozen.DraftTask(); err == nil {
		t.Fatal("empty compiler projected")
	}
	if _, err := PublishBackgroundCleanupPreviewTask(context.Background(), frozen, nil); err == nil {
		t.Fatal("empty compiler published")
	}
	if _, err := VerifyBackgroundCleanupPreviewPublication(context.Background(), frozen, PreparedExposureTask{}); err == nil {
		t.Fatal("unowned result verified")
	}
	if _, err := PrepareBackgroundCleanupPreviewCommitInputs(context.Background(), frozen, PreparedExposureTask{}); err == nil {
		t.Fatal("empty commit prepared")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishBackgroundCleanupPreviewTask(ctx, frozen, nil); err != context.Canceled {
		t.Fatal("lost cancellation", err)
	}
}

// Tests transport publication from a frozen dependency inventory. Dependencies
// are synthetic refs, not a complete source-document/compiler/SQL fixture.
func TestBackgroundCleanupTransportPublishesAndRebuildsOwnedInventory(t *testing.T) {
	p := backgroundCleanupParamsFixture()
	raw, digest, err := FreezeBackgroundCleanupPreviewParams(p)
	if err != nil {
		t.Fatal(err)
	}
	run := uuid.New()
	task := uuid.NewSHA1(run, []byte("semantic.preview_background_cleanup:"+p.TargetID.String()))
	params := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: digest}
	entries := []v2input.Entry{{Ref: params, Contract: "Params"}, {Ref: p.Plan, Contract: "SemanticPlan"},
		{Ref: p.Intent, Contract: "EditIntent"}, {Ref: p.Candidate, Contract: "TransferCandidate"},
		{Ref: p.Source.Source, Contract: "WorkingImage"}, {Ref: p.Protection, Contract: "MaskRef"}}
	for _, contract := range []string{"MaskRef", "ResolvedTransform", "WorkingImage", "NpyArray", "NpyArray", "NpyArray", "NpyArray"} {
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}, Contract: contract})
	}
	closure, err := v2input.FreezeInputInventory(task, "semantic.preview_background_cleanup", raw, params, entries)
	if err != nil {
		t.Fatal(err)
	}
	frozen := FrozenBackgroundCleanupPreviewTask{frozen: FrozenPreviewTask{draft: v2input.DraftTask{
		ID: task, Operation: "semantic.preview_background_cleanup", OperationVersion: "1", ResourceClass: "gpu",
		ParamsArtifactID: params.ArtifactID, ParamsSHA: params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: json.RawMessage(`{}`)}, params: params, closure: closure}}
	for _, failure := range []string{"", "error", "sha", "identity", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			publisher := &previewPublicationRecorder{failure: failure}
			if failure == "cancel" {
				publisher.cancel = cancel
			}
			result, err := PublishBackgroundCleanupPreviewTask(ctx, frozen, publisher)
			if failure != "" {
				if err == nil || result.Draft.ID != uuid.Nil || len(publisher.calls) != 1 {
					t.Fatal("partial publication escaped", result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			payload, err := PrepareBackgroundCleanupPreviewCommitInputs(ctx, frozen, result)
			if err != nil {
				t.Fatal(err)
			}
			var inventory ExposureInventoryBinding
			var transport ExposureTransportBinding
			var node v2input.DraftTask
			if json.Unmarshal(payload.Inventory, &inventory) != nil || json.Unmarshal(payload.Transport, &transport) != nil || json.Unmarshal(payload.Node, &node) != nil ||
				inventory.TaskID != task || transport.TaskID != task || node.ID != task || node.Operation != "semantic.preview_background_cleanup" || len(inventory.Entries) != len(entries) ||
				transport.EntriesSHA != result.Graph.Built.SHA256 || string(payload.ParamsJCS) != string(raw) {
				t.Fatal("transport projection lost own operation or complete inventory")
			}
			if len(publisher.calls) != 3 || publisher.calls[0] != "Params" || publisher.calls[1] != "InputClosureChunk" || publisher.calls[2] != "InputClosureManifest" {
				t.Fatal("wrong publication order", publisher.calls)
			}
			changed := result
			changed.Graph.Built.Count++
			if bad, err := PrepareBackgroundCleanupPreviewCommitInputs(ctx, frozen, changed); err == nil || len(bad.Node) != 0 {
				t.Fatal("caller-mutated transport accepted")
			}
		})
	}
}
