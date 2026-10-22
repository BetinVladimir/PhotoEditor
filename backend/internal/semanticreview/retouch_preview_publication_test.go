package semanticreview

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRetouchPublicationRejectsEmptyOrCancelledCompiler(t *testing.T) {
	var frozen FrozenRetouchPreviewTask
	if _, err := frozen.DraftTask(); err == nil {
		t.Fatal("empty compiler projected")
	}
	if _, err := PublishRetouchPreviewTask(context.Background(), frozen, nil); err == nil {
		t.Fatal("empty compiler published")
	}
	if _, err := VerifyRetouchPreviewPublication(context.Background(), frozen, PreparedExposureTask{}); err == nil {
		t.Fatal("unowned result verified")
	}
	if _, err := PrepareRetouchPreviewCommitInputs(context.Background(), frozen, PreparedExposureTask{}); err == nil {
		t.Fatal("empty commit prepared")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishRetouchPreviewTask(ctx, frozen, nil); err != context.Canceled {
		t.Fatal("lost cancellation", err)
	}
}

// Tests transport publication from a frozen dependency inventory. Dependencies
// are synthetic refs, not a complete source-document/compiler/SQL fixture.
func TestRetouchTransportPublishesAndRebuildsOwnedInventory(t *testing.T) {
	p := retouchParamsFixture()
	raw, digest, err := FreezeRetouchPreviewParams(p)
	if err != nil {
		t.Fatal(err)
	}
	run := uuid.New()
	task := uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+p.TargetID.String()))
	params := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: digest}
	entries := []v2input.Entry{{Ref: params, Contract: "Params"}, {Ref: p.Plan, Contract: "SemanticPlan"},
		{Ref: p.Intent, Contract: "EditIntent"}, {Ref: p.Candidate, Contract: "TransferCandidate"},
		{Ref: p.Source.Source, Contract: "WorkingImage"}, {Ref: p.Protection, Contract: "MaskRef"}}
	for _, contract := range []string{"MaskRef", "ResolvedTransform", "WorkingImage", "NpyArray", "NpyArray", "NpyArray", "NpyArray", "RetouchPreviewAdapterContext", "Face68"} {
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}, Contract: contract})
	}
	closure, err := v2input.FreezeInputInventory(task, "semantic.preview_retouch_region", raw, params, entries)
	if err != nil {
		t.Fatal(err)
	}
	frozen := FrozenRetouchPreviewTask{frozen: FrozenPreviewTask{draft: v2input.DraftTask{
		ID: task, Operation: "semantic.preview_retouch_region", OperationVersion: "1", ResourceClass: "cpu",
		ParamsArtifactID: params.ArtifactID, ParamsSHA: params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: json.RawMessage(`{}`)}, params: params, closure: closure}}
	// Failure at any physical publication boundary must hide the whole draft.
	for _, stage := range []int{1, 2, 3} {
		for _, failure := range []string{"error", "sha", "cancel"} {
			t.Run(fmt.Sprintf("stage_%d_%s", stage, failure), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				publisher := &previewPublicationRecorder{failure: failure, failAt: stage}
				if failure == "cancel" { publisher.cancel = cancel }
				got, err := PublishRetouchPreviewTask(ctx, frozen, publisher)
				if err == nil || got.Draft.ID != uuid.Nil || len(publisher.calls) != stage {
					t.Fatal("partial publication escaped or continued after failure", stage, failure, err, publisher.calls)
				}
				if failure == "cancel" && err != context.Canceled { t.Fatal("cancellation replaced", err) }
			})
		}
	}
	for _, failure := range []string{"", "error", "sha", "identity", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			publisher := &previewPublicationRecorder{failure: failure}
			if failure == "cancel" {
				publisher.cancel = cancel
			}
			result, err := PublishRetouchPreviewTask(ctx, frozen, publisher)
			if failure != "" {
				if err == nil || result.Draft.ID != uuid.Nil || len(publisher.calls) != 1 {
					t.Fatal("partial publication escaped", result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			payload, err := PrepareRetouchPreviewCommitInputs(ctx, frozen, result)
			if err != nil {
				t.Fatal(err)
			}
			var inventory ExposureInventoryBinding
			var transport ExposureTransportBinding
			var node v2input.DraftTask
			if json.Unmarshal(payload.Inventory, &inventory) != nil || json.Unmarshal(payload.Transport, &transport) != nil || json.Unmarshal(payload.Node, &node) != nil ||
				inventory.TaskID != task || transport.TaskID != task || node.ID != task || node.Operation != "semantic.preview_retouch_region" || len(inventory.Entries) != len(entries) ||
				transport.EntriesSHA != result.Graph.Built.SHA256 || string(payload.ParamsJCS) != string(raw) {
				t.Fatal("transport projection lost own operation or complete inventory")
			}
			if len(publisher.calls) != 3 || publisher.calls[0] != "Params" || publisher.calls[1] != "InputClosureChunk" || publisher.calls[2] != "InputClosureManifest" {
				t.Fatal("wrong publication order", publisher.calls)
			}
			changed := result
			changed.Graph.Built.Count++
			if bad, err := PrepareRetouchPreviewCommitInputs(ctx, frozen, changed); err == nil || len(bad.Node) != 0 {
				t.Fatal("caller-mutated transport accepted")
			}
		})
	}
}
