package provenance

import (
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// No entries disappear: Controls retain recipe/style/mask/report/array evidence
// for operation-specific authority. This inventory grants no original-source or
// producer claim; the final resolver must bind every content category below.
type SigningRenderCausalInventory struct {
	taskDigest                           string
	Images, Results, Models, Backgrounds []v2input.Ref
	Controls                             []v2input.Entry
}

func CompileSigningRenderCausalInventory(task CanonicalSigningTask, render SigningRenderEvidence, closure SigningRenderClosureEvidence) (SigningRenderCausalInventory, error) {
	var zero SigningRenderCausalInventory
	digest, err := canonical.Hash(task)
	if err != nil || task.Operation != "render.compose" || task.Version != "1" || digest != render.taskDigest || digest != closure.taskDigest {
		return zero, ErrInvalid
	}
	if err := verifySigningRenderInputDigest(task, closure.inventory.Entries); err != nil {
		return zero, err
	}
	// Join the independently read recipe and closure receipts. Every direct
	// dependency must exist both as a typed pin and as the same scoped occurrence.
	if len(render.dependencies) < 2 || len(render.dependencies) > 4096 {
		return zero, ErrInvalid
	}
	for _, dependency := range render.dependencies {
		if dependency.ParentRef != task.Params || !ref(dependency.Ref) {
			return zero, ErrInvalid
		}
		entryFound := false
		for _, entry := range closure.inventory.Entries {
			if entry.Ref == dependency.Ref && entry.Contract == dependency.Contract {
				entryFound = true
				break
			}
		}
		if !entryFound {
			return zero, ErrInvalid
		}
		expected, err := signingRenderDependencyDigest(dependency)
		if err != nil {
			return zero, ErrInvalid
		}
		occurrenceFound := false
		for _, occurrence := range closure.inventory.Occurrences {
			node := occurrence.Node
			if node.Ref != dependency.Ref || node.ParentRef != task.Params || node.Contract != dependency.Contract {
				continue
			}
			actual, err := signingRenderDependencyDigest(node)
			if err != nil {
				return zero, ErrInvalid
			}
			if actual == expected {
				occurrenceFound = true
				break
			}
		}
		if !occurrenceFound {
			return zero, ErrInvalid
		}
	}
	out := SigningRenderCausalInventory{taskDigest: digest, Images: []v2input.Ref{}, Results: []v2input.Ref{}, Models: []v2input.Ref{}, Backgrounds: []v2input.Ref{}, Controls: []v2input.Entry{}}
	contains := func(refs []v2input.Ref, want v2input.Ref) bool {
		for _, r := range refs {
			if r == want {
				return true
			}
		}
		return false
	}
	params := false
	for _, entry := range closure.inventory.Entries {
		switch entry.Contract {
		case "WorkingImage":
			out.Images = append(out.Images, entry.Ref)
		case "ImageResult":
			out.Results = append(out.Results, entry.Ref)
		case "CandidateProfile":
			out.Models = append(out.Models, entry.Ref)
		case "StyleBackgroundAsset":
			out.Backgrounds = append(out.Backgrounds, entry.Ref)
		default:
			out.Controls = append(out.Controls, entry)
		}
		if entry.Contract == "FrozenRenderRecipe" && entry.Ref == task.Params {
			params = true
		}
	}
	if !params || !contains(out.Images, render.pixels.recipe.Scalars.Source.Source) {
		return zero, ErrInvalid
	}
	if learning := render.pixels.recipe.Learning; learning != nil && !contains(out.Models, learning.Model) {
		return zero, ErrInvalid
	}
	if background := render.pixels.recipe.BackgroundAsset; background != nil && !contains(out.Backgrounds, background.Source) {
		return zero, ErrInvalid
	}
	for _, parent := range render.parents {
		if !contains(out.Results, parent.Root) {
			return zero, ErrInvalid
		}
	}
	return out, nil
}

// Paths are traversal diagnostics: the closure root can use a different prefix.
// All authority fields, including expected binding/model/background, remain bound.
func signingRenderDependencyDigest(node v2draft.LayerRef) (string, error) {
	node.Path = ""
	return canonical.Hash(node)
}
