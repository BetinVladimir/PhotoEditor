package provenance

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// This resolver uses the qualified historical causal source, not caller-supplied
// inventory alone. Model/control bytes are bound by frozen Params and closure;
// encoded backgrounds contribute original content sources to the graph.
type BoundSigningRenderDependencyResolver struct {
	Causal BoundSigningRenderCausalSource
}

func (s BoundSigningRenderDependencyResolver) ResolveSigningRenderDependencies(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, inventory SigningRenderCausalInventory) ([]SigningRenderDependencyClaim, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := s.Causal.read(ctx, who, ready, task)
	if err != nil {
		return nil, err
	}
	expected, err := canonical.Hash(inventory)
	if err != nil {
		return nil, err
	}
	actual, err := canonical.Hash(current.inventory)
	if err != nil || actual != expected || inventory.taskDigest != current.inventory.taskDigest {
		return nil, ErrInvalid
	}
	return compileSigningRenderDependencyClaims(current.inventory)
}
func compileSigningRenderDependencyClaims(inventory SigningRenderCausalInventory) ([]SigningRenderDependencyClaim, error) {
	claims := []SigningRenderDependencyClaim{}
	for _, pin := range inventory.Models {
		claims = append(claims, SigningRenderDependencyClaim{Entry: v2input.Entry{Ref: pin, Contract: "CandidateProfile"}, Kind: "parameter"})
	}
	for _, pin := range inventory.Backgrounds {
		claims = append(claims, SigningRenderDependencyClaim{Entry: v2input.Entry{Ref: pin, Contract: "StyleBackgroundAsset"}, Kind: "source"})
	}
	for _, entry := range inventory.Controls {
		switch entry.Contract {
		case "FrozenRenderRecipe", "StyleV2", "CorrectionPlan", "Override", "LayerStack", "Params", "RasterizedMask", "ManualMask", "MaskRef", "NpyArray", "DescriptorShard", "FaceObservations", "IdentityReport", "ImageReport":
			claims = append(claims, SigningRenderDependencyClaim{Entry: entry, Kind: "parameter"})
		default:
			return nil, ErrInvalid
		}
	}
	for _, claim := range claims {
		if !ref(claim.Entry.Ref) {
			return nil, ErrInvalid
		}
	}
	return claims, nil
}

var _ SigningRenderDependencyResolver = BoundSigningRenderDependencyResolver{}
