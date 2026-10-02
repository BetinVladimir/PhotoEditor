package v2render

import (
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// LayerComposeParams is the frozen worker wire contract. Nullable fields are
// always serialized: null is distinct from an omitted lineage/protection field.
type LayerComposeParams struct {
	SchemaVersion  int                           `json:"schema_version"`
	Base           v2input.SessionVersionBinding `json:"base"`
	SourceResult   *v2input.Ref                  `json:"source_result"`
	ProposalResult v2input.Ref                   `json:"proposal_result"`
	Mask           json.RawMessage               `json:"mask"`
	ProtectedMask  *v2input.Ref                  `json:"protected_mask"`
	Opacity        float64                       `json:"opacity"`
	Blend          string                        `json:"blend"`
}

type CompiledComposition struct {
	Params LayerComposeParams
	JSON   []byte
	Budget LayerBudget
}

func validLayerArtifactRef(ref v2input.Ref) bool {
	raw, err := hex.DecodeString(ref.SHA256)
	return ref.ArtifactID != uuid.Nil && err == nil && len(raw) == 32 && hex.EncodeToString(raw) == ref.SHA256
}

// CompileComposition freezes one same-geometry composition. Artifact resolution
// must already establish parent images/canvas/color; SQL admission must prove a
// null source result is the authorized original source. This does not enqueue.
func CompileComposition(layer Layer, base v2input.SessionVersionBinding, sourceResult *v2input.Ref, proposalResult v2input.Ref, protected *v2input.Ref, width, height int64) (CompiledComposition, error) {
	if !layer.Active() || !validLayerArtifactRef(base.Source) || !validLayerArtifactRef(proposalResult) || (sourceResult != nil && !validLayerArtifactRef(*sourceResult)) || (protected != nil && !validLayerArtifactRef(*protected)) {
		return CompiledComposition{}, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "source": base, "layers": []Layer{layer}})
	if err != nil {
		return CompiledComposition{}, err
	}
	plan, err := Compile(raw, base)
	if err != nil {
		return CompiledComposition{}, err
	}
	budget, err := plan.BudgetLayer(layer.ID, width, height, protected, 512)
	if err != nil {
		return CompiledComposition{}, err
	}
	clone := func(ref *v2input.Ref) *v2input.Ref {
		if ref == nil {
			return nil
		}
		owned := *ref
		return &owned
	}
	mask, err := canonical.Bytes(layer.Mask)
	if err != nil {
		return CompiledComposition{}, err
	}
	params := LayerComposeParams{SchemaVersion: 1, Base: base, SourceResult: clone(sourceResult), ProposalResult: proposalResult, Mask: mask, ProtectedMask: clone(protected), Opacity: layer.Opacity, Blend: layer.Blend}
	body, err := canonical.Marshal(params)
	if err != nil {
		return CompiledComposition{}, err
	}
	return CompiledComposition{Params: params, JSON: body, Budget: budget}, nil
}
