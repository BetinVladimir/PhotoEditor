package v2region

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type RegionInputResolver interface {
	ResolveRegionInputs(context.Context, auth.Identity, uuid.UUID, v2input.SessionVersionBinding, v2input.Ref, v2draft.CorrectionVector) ([]v2input.Entry, error)
}

// ResolveInputs reads identity and recipe binding from frozen plan bytes rather
// than the mutable PreparedRegion fields. It returns a content inventory only;
// SQL admission must recheck scope and pin all catalog versions.
func (p FrozenRegionPlan) ResolveInputs(ctx context.Context, resolver RegionInputResolver, who auth.Identity) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resolver == nil || len(p.body) == 0 {
		return nil, ErrRegionInvalid
	}
	document, err := p.decode()
	if err != nil {
		return nil, err
	}

	resolved, err := resolver.ResolveRegionInputs(ctx, who, document.TenantID, document.FrozenRecipe.Source, document.FrozenRecipe.BaseStyle, document.FrozenRecipe.EffectiveDelta)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(resolved) < 3 || len(resolved) > 511 {
		return nil, ErrRegionInvalid
	}
	lookup := make(map[uuid.UUID]v2input.Entry, len(resolved)+1)
	for _, entry := range resolved {
		if entry.Ref.ArtifactID == uuid.Nil || len(entry.Contract) == 0 || len(entry.Contract) > 120 {
			return nil, ErrRegionInvalid
		}
		// Reuse strict lowercase digest validation of the public artifact reference.
		if len(entry.Ref.SHA256) != 64 {
			return nil, ErrRegionInvalid
		}
		for _, c := range entry.Ref.SHA256 {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return nil, ErrRegionInvalid
			}
		}
		if prior, ok := lookup[entry.Ref.ArtifactID]; ok && prior != entry {
			return nil, ErrRegionInvalid
		}
		lookup[entry.Ref.ArtifactID] = entry
	}
	recipe := v2input.Entry{Ref: document.Recipe.Ref, Contract: "FrozenRecipe"}
	if prior, ok := lookup[recipe.Ref.ArtifactID]; ok && prior != recipe {
		return nil, ErrRegionInvalid
	}
	lookup[recipe.Ref.ArtifactID] = recipe
	for _, required := range document.Inputs {
		if lookup[required.Ref.ArtifactID] != required {
			return nil, ErrRegionInvalid
		}
	}
	result := make([]v2input.Entry, 0, len(lookup))
	for _, entry := range lookup {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].Ref.ArtifactID[:], result[j].Ref.ArtifactID[:]) < 0
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
