package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningRenderGraphRequiresEveryDependencyDisposition(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "duplicate", "unknown", "background_parameter", "self", "changed_task", "child", "ownership", "missing_pixel", "foreign_pixel", "duplicate_pixel", "pixel_control_alias", "output_as_input", "missing_model_lock"} {
		t.Run(mode, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
			root, image, model, background, control := pin(), pin(), pin(), pin(), pin()
			outputs, _ := canonical.Marshal([]v2input.Ref{root})
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: sha, ResultDigest: sha, Params: control, Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
			digest, _ := canonical.Hash(task)
			producer := uuid.New()
			receipt := signingRenderReviewedCausalReceipt{causal: signingRenderCausalReceipt{stamp: sha, renderDigest: sha, inventory: SigningRenderCausalInventory{taskDigest: digest, Images: []v2input.Ref{image}, Models: []v2input.Ref{model}, Backgrounds: []v2input.Ref{background}, Controls: []v2input.Entry{{Ref: control, Contract: "FrozenRenderRecipe"}}}, pixels: signingRenderPixelAncestry{inputs: []v2input.Ref{image}, producers: []uuid.UUID{producer}}}, outputs: ResolvedSigningRenderOutputs{Action: SigningAction{Operation: task.Operation, Version: task.Version, ParamsSHA: control.SHA256, ModelLockSHA: &sha}, Children: []SigningChildOutput{{Root: root, Ref: pin(), Role: "working_image"}, {Root: root, Ref: pin(), Role: "changed_mask"}, {Root: root, Ref: pin(), Role: "report"}}}}
			claims := []SigningRenderDependencyClaim{{Entry: v2input.Entry{Ref: model, Contract: "CandidateProfile"}, Kind: "parameter"}, {Entry: v2input.Entry{Ref: background, Contract: "StyleBackgroundAsset"}, Kind: "source"}, {Entry: v2input.Entry{Ref: control, Contract: "FrozenRenderRecipe"}, Kind: "parameter"}}
			switch mode {
			case "missing_pixel":
				receipt.causal.inventory.Results = append(receipt.causal.inventory.Results, pin())
			case "foreign_pixel":
				receipt.causal.pixels.inputs[0] = pin()
			case "duplicate_pixel":
				receipt.causal.inventory.Results = []v2input.Ref{image}
			case "pixel_control_alias":
				receipt.causal.inventory.Models[0] = image
				claims[0].Entry.Ref = image
			case "output_as_input":
				receipt.outputs.Children[0].Ref = image
			case "missing_model_lock":
				receipt.outputs.Action.ModelLockSHA = nil
			case "missing":
				claims = claims[:2]
			case "duplicate":
				claims[2] = claims[0]
			case "unknown":
				claims[2].Entry.Ref = pin()
			case "background_parameter":
				claims[1].Kind = "parameter"
			case "self":
				claims[0].Kind = "producer"
				claims[0].Producer = task.Task
			case "changed_task":
				task.Result = uuid.New()
			case "child":
				receipt.outputs.Children = receipt.outputs.Children[:2]
			}
			got, err := compileSigningRenderGraphTask(task, receipt, claims)
			if mode == "valid" || mode == "ownership" {
				if err != nil || len(got.Sources) != 1 || got.Sources[0] != background || len(got.Node.Inputs) != 2 || len(got.Node.Parents) != 1 || got.Node.Parents[0] != producer {
					t.Fatal("render graph boundary rejected", err)
				}
				receipt.outputs.Children[0].Ref.SHA256 = "changed"
				sha = "changed"
				if got.Node.Children[0].Ref.SHA256 == "changed" || *got.Node.Action.ModelLockSHA == "changed" {
					t.Fatal("graph aliases receipt")
				}
			} else if err == nil || got.Node.Inputs != nil {
				t.Fatal("incomplete dependency graph accepted", err)
			}
		})
	}
}

type signingReviewedCausalFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (signingRenderReviewedCausalReceipt, error)

func (f signingReviewedCausalFunc) read(c context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask) (signingRenderReviewedCausalReceipt, error) {
	return f(c, w, r, t)
}

type signingDependencyFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, SigningRenderCausalInventory) ([]SigningRenderDependencyClaim, error)

func (f signingDependencyFunc) ResolveSigningRenderDependencies(c context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask, i SigningRenderCausalInventory) ([]SigningRenderDependencyClaim, error) {
	return f(c, w, r, t, i)
}
func signingRenderGraphFixture() (CanonicalSigningTask, signingRenderReviewedCausalReceipt, []SigningRenderDependencyClaim) {
	sha := strings.Repeat("a", 64)
	pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	root, image, model, background, control := pin(), pin(), pin(), pin(), pin()
	outputs, _ := canonical.Marshal([]v2input.Ref{root})
	task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: sha, ResultDigest: sha, Params: control, Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
	digest, _ := canonical.Hash(task)
	producer := uuid.New()
	receipt := signingRenderReviewedCausalReceipt{causal: signingRenderCausalReceipt{stamp: sha, renderDigest: sha, inventory: SigningRenderCausalInventory{taskDigest: digest, Images: []v2input.Ref{image}, Models: []v2input.Ref{model}, Backgrounds: []v2input.Ref{background}, Controls: []v2input.Entry{{Ref: control, Contract: "FrozenRenderRecipe"}}}, pixels: signingRenderPixelAncestry{inputs: []v2input.Ref{image}, producers: []uuid.UUID{producer}}}, outputs: ResolvedSigningRenderOutputs{Action: SigningAction{Operation: task.Operation, Version: task.Version, ParamsSHA: control.SHA256, ModelLockSHA: &sha}, Children: []SigningChildOutput{{Root: root, Ref: pin(), Role: "working_image"}, {Root: root, Ref: pin(), Role: "changed_mask"}, {Root: root, Ref: pin(), Role: "report"}}}}
	claims := []SigningRenderDependencyClaim{{Entry: v2input.Entry{Ref: model, Contract: "CandidateProfile"}, Kind: "parameter"}, {Entry: v2input.Entry{Ref: background, Contract: "StyleBackgroundAsset"}, Kind: "source"}, {Entry: v2input.Entry{Ref: control, Contract: "FrozenRenderRecipe"}, Kind: "parameter"}}
	return task, receipt, claims
}
func TestSigningRenderGraphResolverRechecksAllStages(t *testing.T) {
	for _, mode := range []string{"valid", "stamp", "producer", "review", "dependency_error", "mutated_inventory", "reordered", "principal"} {
		t.Run(mode, func(t *testing.T) {
			task, receipt, claims := signingRenderGraphFixture()
			reads, dependencies := 0, 0
			failure := errors.New("dependency authority unavailable")
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			if mode == "principal" {
				who.UserID = uuid.Nil
			}
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New()}
			resolver := RenderSigningTaskResolver{
				Reviewed: signingReviewedCausalFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (signingRenderReviewedCausalReceipt, error) {
					reads++
					if reads == 2 {
						switch mode {
						case "stamp":
							receipt.causal.stamp = strings.Repeat("b", 64)
						case "producer":
							receipt.causal.pixels.producers[0] = uuid.New()
						case "review":
							id := uuid.New()
							receipt.outputs.Action.ReviewID = &id
						}
					}
					return receipt, nil
				}),
				Dependencies: signingDependencyFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, inventory SigningRenderCausalInventory) ([]SigningRenderDependencyClaim, error) {
					dependencies++
					if mode == "dependency_error" {
						return nil, failure
					}
					if mode == "mutated_inventory" {
						inventory.Models[0].SHA256 = "changed"
					}
					got := append([]SigningRenderDependencyClaim(nil), claims...)
					if mode == "reordered" && dependencies == 2 {
						got[0], got[2] = got[2], got[0]
					}
					return got, nil
				}),
			}
			got, err := resolver.ResolveSigningTaskLineage(context.Background(), who, ready, task)
			if mode == "valid" || mode == "mutated_inventory" || mode == "reordered" {
				if err != nil || reads != 2 || dependencies != 2 || len(got.Node.Inputs) != 2 {
					t.Fatal("valid render resolver rejected", err)
				}
			} else {
				if err == nil || got.Node.Inputs != nil {
					t.Fatal("unstable graph accepted", err)
				}
				if mode == "dependency_error" && !errors.Is(err, failure) {
					t.Fatal("dependency error hidden", err)
				}
				if mode == "principal" && reads != 0 {
					t.Fatal("invalid principal reached adapters")
				}
			}
		})
	}
}
