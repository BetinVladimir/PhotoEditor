package v2draft

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"sort"
	"strconv"
)

// ResolveTaskInputs expands trusted compiler roots through the same scoped
// catalog used by drafts. It returns a sorted immutable inventory, not a task
// authorization or registration. SQL must revalidate scope at graph admission.
func (s ClosureResolver) ResolveTaskInputs(ctx context.Context, who auth.Identity, tenantID uuid.UUID, roots []v2input.Entry, binding v2input.SessionVersionBinding) ([]v2input.Entry, error) {
	if len(roots) < 2 || len(roots) > 260 || binding.AssetVersionID == uuid.Nil || !isDocumentRef(binding.Source) || !isDocumentRef(v2input.Ref{ArtifactID: binding.AssetVersionID, SHA256: binding.CanvasHash}) {
		return nil, v2input.ErrInvalid
	}
	fullRenderRoots := 0
	for _, root := range roots {
		if root.Contract == "FrozenRenderRecipe" {
			fullRenderRoots++
		}
	}
	if fullRenderRoots > 0 {
		if fullRenderRoots != 1 || len(roots) != 2 || roots[0].Ref.ArtifactID == roots[1].Ref.ArtifactID {
			return nil, v2input.ErrInvalid
		}
		for _, root := range roots {
			if root.Contract != "FrozenRenderRecipe" && root.Contract != "WorkingImage" {
				return nil, v2input.ErrInvalid
			}
		}
	}
	nodes := []LayerRef{}
	sourceFound := false
	for i, root := range roots {
		if !isDocumentRef(root.Ref) || (root.Contract != "Params" && root.Contract != "WorkingImage" && root.Contract != "ImageResult" && root.Contract != "MaskRef" && root.Contract != "FrozenRenderRecipe") {
			return nil, v2input.ErrInvalid
		}
		node := LayerRef{Path: "TaskInputs[" + strconv.Itoa(i) + "]", Contract: root.Contract, Ref: root.Ref}
		if root.Ref == binding.Source && root.Contract == "WorkingImage" {
			if sourceFound {
				return nil, v2input.ErrInvalid
			}
			node.CanvasHash = binding.CanvasHash
			nodes = append([]LayerRef{node}, nodes...)
			sourceFound = true
		} else {
			nodes = append(nodes, node)
		}
	}
	if !sourceFound {
		return nil, v2input.ErrInvalid
	}
	inventory := []v2input.Entry{}
	if _, err := s.resolveRootsWithInventory(ctx, who, tenantID, nodes, binding, &inventory); err != nil {
		return nil, err
	}
	return uniqueTaskInputs(inventory)
}

func uniqueTaskInputs(inventory []v2input.Entry) ([]v2input.Entry, error) {
	unique := map[uuid.UUID]v2input.Entry{}
	for _, entry := range inventory {
		prior, exists := unique[entry.Ref.ArtifactID]
		if exists {
			if prior.Ref != entry.Ref || prior.Contract != entry.Contract && prior.Contract != "Artifact" && entry.Contract != "Artifact" {
				return nil, v2input.ErrInvalid
			}
			if entry.Contract == "Artifact" {
				continue
			}
		}
		unique[entry.Ref.ArtifactID] = entry
	}
	result := make([]v2input.Entry, 0, len(unique))
	for _, entry := range unique {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].Ref.ArtifactID[:], result[j].Ref.ArtifactID[:]) < 0
	})
	return result, nil
}
