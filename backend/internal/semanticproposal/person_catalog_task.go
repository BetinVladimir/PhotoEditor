package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type catalogPersonPreparation struct {
	frozen v2input.FrozenSemanticPersonTask
	inputs string // immutable canonical exact catalog evidence
}

// prepareCatalogPersonTask derives the input descriptors from verified source
// metadata, then reads exact sizes through contract-scoped catalog authorization.
// Original source pixels/ICC, model/report qualifications and locked admission
// must independently accompany this preparatory value before queue activation.
func prepareCatalogPersonTask(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenantID, workflow uuid.UUID, ordinal int, binding v2input.SessionVersionBinding, imageBody json.RawMessage, params v2input.Ref, modelLock string, guidedTile int, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (catalogPersonPreparation, error) {
	var zero catalogPersonPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	// Own source bytes before catalog callbacks; no external mutable descriptors.
	imageBody = append([]byte(nil), imageBody...)
	prepared, err := v2input.PrepareSemanticPersonParams(ctx, binding, imageBody, modelLock, guidedTile)
	if err != nil {
		return zero, err
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, imageBody)
	if err != nil {
		return zero, err
	}
	nodes := []v2draft.LayerRef{{Ref: params, Contract: "Params"}}
	sizes := []v2input.SemanticInputSize{{Ref: params, Bytes: int64(len(prepared.Params))}}
	for _, entry := range prepared.Inputs {
		node := v2draft.LayerRef{Ref: entry.Ref, Contract: entry.Contract}
		if entry.Ref == binding.Source {
			node.CanvasHash = binding.CanvasHash
		} else {
			node.ParentRef = binding.Source
			switch {
			case entry.Ref == metadata.Inputs.Pixels:
				node.Contract = "WorkingPixels"
			case metadata.Inputs.Alpha != nil && entry.Ref == *metadata.Inputs.Alpha:
				node.Contract = "WorkingAlpha"
			default:
				node.Contract = "DenseWarpField"
			}
		}
		nodes = append(nodes, node)
		sizes = append(sizes, v2input.SemanticInputSize{Ref: entry.Ref, Bytes: 1})
	}
	// Reject source/Params/shape/capacity contradictions before catalog IO.
	if _, err := v2input.CompileSemanticPersonTask(ctx, workflow, ordinal, binding, imageBody, params, modelLock, guidedTile, sizes, costs, ram, vram, scratch, output); err != nil {
		return zero, err
	}
	evidence, err := v2draft.ReadInputEvidence(ctx, catalog, who, tenantID, nodes)
	if err != nil {
		return zero, err
	}
	actual := make([]v2input.SemanticInputSize, len(evidence))
	for i, entry := range evidence {
		actual[i] = v2input.SemanticInputSize{Ref: entry.Ref, Bytes: entry.Object.Bytes}
	}
	for _, size := range actual {
		if (size.Ref == params && size.Bytes != int64(len(prepared.Params))) || (size.Ref == binding.Source && size.Bytes != int64(len(imageBody))) {
			return zero, v2input.ErrInvalid
		}
	}
	frozen, err := v2input.CompileSemanticPersonTask(ctx, workflow, ordinal, binding, imageBody, params, modelLock, guidedTile, actual, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	pins, err := canonical.Marshal(evidence)
	if err != nil || len(pins) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	if _, err := decodePersonInputEvidence(ctx, frozen, string(pins)); err != nil {
		return zero, err
	}
	return catalogPersonPreparation{frozen: frozen, inputs: string(pins)}, nil
}
