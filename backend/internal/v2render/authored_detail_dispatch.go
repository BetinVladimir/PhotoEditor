package v2render

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// resumeScopedDetail builds every current document from actual scoped reads.
// The only external task identifiers select independently adopted mask tasks;
// they do not contain replacement regions, effective settings, or mask pixels.
func (s SQLAuthoredDetailMaterialization) resumeScopedDetail(ctx context.Context, who auth.Identity, source AuthoredInstructionSource,
	styleBody []byte, witnesses map[string]string, original, current []byte, transformTasks []uuid.UUID, protected *v2input.Ref) (bool, error) {
	if len(transformTasks) > 32896 {
		return false, v2input.ErrInvalid
	}
	proofs := make([]CurrentAuthoredDetailTransform, 0, len(transformTasks))
	documents := make([]AuthoredDetailTransformDocument, 0, len(transformTasks))
	seen := map[uuid.UUID]bool{}
	for _, task := range transformTasks {
		if task == uuid.Nil || seen[task] {
			return false, v2input.ErrInvalid
		}
		seen[task] = true
		proof, found, err := s.Transforms.Read(ctx, who, source, task)
		if err != nil {
			return false, err
		}
		if !found {
			return false, ErrAuthoredSegmentPending
		}
		proofs = append(proofs, proof)
		documents = append(documents, proof.Document())
	}
	var params struct {
		Regions []struct {
			Mask json.RawMessage `json:"mask"`
		} `json:"regions"`
		Protected []json.RawMessage `json:"protected"`
	}
	if json.Unmarshal(original, &params) != nil {
		return false, v2input.ErrInvalid
	}
	masks := make([]json.RawMessage, 0, len(params.Regions)+len(params.Protected))
	for _, region := range params.Regions {
		masks = append(masks, region.Mask)
	}
	masks = append(masks, params.Protected...)
	_, detailUsed, err := compileAuthoredMaskPaths(masks, current, source.Source.Source, documents)
	if err != nil {
		return false, err
	}
	detailDocuments := make([]AuthoredDetailTransformDocument, 0, len(detailUsed))
	detailProofs := make([]json.RawMessage, 0, len(detailUsed))
	allUsed := map[int]bool{}
	for _, i := range detailUsed {
		detailDocuments = append(detailDocuments, documents[i])
		witness, err := proofs[i].Witness()
		if err != nil {
			return false, err
		}
		detailProofs = append(detailProofs, witness)
		allUsed[i] = true
	}
	body, err := CompileAuthoredDetailOccurrence(original, current, source.Source.Source, detailDocuments)
	if err != nil {
		return false, err
	}
	var stack struct {
		Layers []struct {
			ID   uuid.UUID       `json:"id"`
			Mask json.RawMessage `json:"mask"`
		} `json:"layers"`
	}
	if json.Unmarshal([]byte(source.LayerStackJCS), &stack) != nil || len(source.Instruction.Layers) != 1 {
		return false, v2input.ErrInvalid
	}
	var expression json.RawMessage
	count := 0
	for _, layer := range stack.Layers {
		if layer.ID == source.Instruction.Layers[0] {
			expression = layer.Mask
			count++
		}
	}
	if count != 1 {
		return false, v2input.ErrInvalid
	}
	bindings, maskUsed, err := s.publishLayerMasks(ctx, who, source, current, expression, proofs)
	if err != nil {
		return false, err
	}
	for _, i := range maskUsed {
		allUsed[i] = true
	}
	publishedProtection, protectionUsed, err := s.publishRecipeProtection(ctx, who, source, current, proofs)
	if err != nil {
		return false, err
	}
	for _, i := range protectionUsed {
		allUsed[i] = true
	}
	if protected != nil && (publishedProtection == nil || *protected != *publishedProtection) {
		return false, v2input.ErrInvalid
	}
	protected = publishedProtection
	if len(allUsed) != len(proofs) {
		return false, v2input.ErrInvalid
	}
	style, err := s.Continuation.Style.Publish(ctx, source.Tenant, source.Parent, source.Instruction.Ordinal, styleBody, witnesses)
	if err != nil {
		return false, err
	}
	transforms, err := canonical.Marshal(detailProofs)
	if err != nil {
		return false, err
	}
	publishedParams, err := s.Continuation.Params.Publish(ctx, source.Tenant, source.Parent, source.Instruction.Ordinal, body, style, transforms)
	if err != nil {
		return false, err
	}
	layersBody, err := CompileAuthoredCurrentDetailLayers(ctx, source, publishedParams, bindings)
	if err != nil {
		return false, err
	}
	bindingJSON, err := canonical.Marshal(bindings)
	if err != nil {
		return false, err
	}
	layers, err := s.Continuation.Layers.Publish(ctx, source.Tenant, source.Parent, source.Instruction.Ordinal, layersBody, bindingJSON)
	if err != nil {
		return false, err
	}
	return s.Continuation.Segment.Resume(ctx, who, source.Tenant, source.Parent, source.Instruction.Ordinal, &layers, protected)
}
