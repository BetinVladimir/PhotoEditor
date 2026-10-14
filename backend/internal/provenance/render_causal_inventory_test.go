package provenance

import (
	"encoding/binary"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningRenderCausalInventoryRetainsEveryTypedDependency(t *testing.T) {
	entries := []v2input.Entry{}
	for i, contract := range []string{"WorkingImage", "FrozenRenderRecipe", "CandidateProfile", "StyleBackgroundAsset", "ImageResult", "NpyArray", "StyleV2"} {
		var id uuid.UUID
		binary.BigEndian.PutUint64(id[8:], uint64(i+1))
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: id, SHA256: strings.Repeat("a", 64)}, Contract: contract})
	}
	task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1", Params: entries[1].Ref}
	task.InputDigest, _ = canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task.Task, task.Operation, task.Version, entries})
	render := SigningRenderEvidence{}
	render.taskDigest, _ = canonical.Hash(task)
	render.pixels.recipe.Scalars.Source.Source = entries[0].Ref
	render.pixels.recipe.Learning = &v2draft.FrozenRenderLearning{Model: entries[2].Ref}
	render.pixels.recipe.BackgroundAsset = &v2draft.FrozenBackgroundAsset{Source: entries[3].Ref}
	render.parents = []SigningRenderParent{{Root: entries[4].Ref}}
	render.dependencies = []v2draft.LayerRef{
		{Path: "$.recipe.source.source", Contract: "WorkingImage", Ref: entries[0].Ref, ParentRef: task.Params},
		{Path: "$.recipe.base_style", Contract: "StyleV2", Ref: entries[6].Ref, ParentRef: task.Params},
	}
	closure := SigningRenderClosureEvidence{taskDigest: render.taskDigest, inventory: v2draft.TaskInputEvidence{Entries: entries}}
	for _, node := range render.dependencies {
		closure.inventory.Occurrences = append(closure.inventory.Occurrences, v2draft.OccurrenceWitness{Node: node})
	}
	for _, mode := range []string{"missing_occurrence", "wrong_parent", "wrong_contract", "wrong_canvas", "wrong_binding", "path_prefix"} {
		t.Run(mode, func(t *testing.T) {
			changed := closure
			changed.inventory.Occurrences = append([]v2draft.OccurrenceWitness(nil), closure.inventory.Occurrences...)
			switch mode {
			case "missing_occurrence":
				changed.inventory.Occurrences = changed.inventory.Occurrences[:1]
			case "wrong_parent":
				changed.inventory.Occurrences[1].Node.ParentRef.ArtifactID = uuid.New()
			case "wrong_contract":
				changed.inventory.Occurrences[1].Node.Contract = "Override"
			case "wrong_canvas":
				changed.inventory.Occurrences[1].Node.CanvasHash = strings.Repeat("b", 64)
			case "wrong_binding":
				changed.inventory.Occurrences[1].Node.ExpectedBinding = &v2input.SessionVersionBinding{}
			case "path_prefix":
				changed.inventory.Occurrences[1].Node.Path = "$roots[1].recipe.base_style"
			}
			got, err := CompileSigningRenderCausalInventory(task, render, changed)
			if mode == "path_prefix" {
				if err != nil {
					t.Fatal("diagnostic path changed authority", err)
				}
			} else if err == nil || got.Images != nil {
				t.Fatal("incomplete dependency join accepted")
			}
		})
	}
	out, err := CompileSigningRenderCausalInventory(task, render, closure)
	if err != nil || len(out.Images)+len(out.Results)+len(out.Models)+len(out.Backgrounds)+len(out.Controls) != len(entries) {
		t.Fatal("inventory lost dependencies", err)
	}
	entries[0].Ref.SHA256 = strings.Repeat("b", 64)
	if out.Images[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatal("inventory aliases source entries")
	}
	if _, err := CompileSigningRenderCausalInventory(task, render, closure); err == nil {
		t.Fatal("changed inventory digest accepted")
	}
	entries[0].Ref.SHA256 = strings.Repeat("a", 64)
	render.pixels.recipe.Learning.Model.ArtifactID = uuid.New()
	if _, err := CompileSigningRenderCausalInventory(task, render, closure); err == nil {
		t.Fatal("missing selected model accepted")
	}
}
