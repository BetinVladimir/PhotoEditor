package v2render

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// publishLayerMasks retains actual standalone MaskRef documents. It never puts
// a transformed-mask result root into the layer-expression ArtifactRef slot.
func (s SQLAuthoredDetailMaterialization) publishLayerMasks(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, currentImage []byte, expression json.RawMessage, proofs []CurrentAuthoredDetailTransform) ([]AuthoredCurrentDetailMaskBinding, []int, error) {
	fail := func() ([]AuthoredCurrentDetailMaskBinding, []int, error) { return nil, nil, v2input.ErrInvalid }
	if s.Masks.Scheduler == nil || s.Masks.Scheduler != s.Continuation.Style.Scheduler || s.Masks.Objects == nil || s.Masks.Bucket != s.Original.Bucket {
		return fail()
	}
	refs := []v2input.Ref{}
	seen := map[v2input.Ref]bool{}
	nodes := 0
	var walk func(json.RawMessage, int) error
	walk = func(raw json.RawMessage, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		nodes++
		if depth > 128 || nodes > 65535 {
			return v2input.ErrInvalid
		}
		var node map[string]json.RawMessage
		if json.Unmarshal(raw, &node) != nil {
			return v2input.ErrInvalid
		}
		var op string
		if json.Unmarshal(node["op"], &op) != nil {
			return v2input.ErrInvalid
		}
		switch op {
		case "ref":
			var ref v2input.Ref
			if json.Unmarshal(node["mask"], &ref) != nil || !validLayerArtifactRef(ref) {
				return v2input.ErrInvalid
			}
			if !seen[ref] {
				if len(refs) >= 256 {
					return v2input.ErrInvalid
				}
				seen[ref] = true
				refs = append(refs, ref)
			}
		case "invert":
			return walk(node["operand"], depth+1)
		case "union", "intersect", "subtract":
			if err := walk(node["left"], depth+1); err != nil {
				return err
			}
			return walk(node["right"], depth+1)
		default:
			return v2input.ErrInvalid
		}
		return nil
	}
	if err := walk(expression, 0); err != nil {
		return nil, nil, err
	}
	documents := make([]AuthoredDetailTransformDocument, len(proofs))
	for i, p := range proofs {
		documents[i] = p.Document()
	}
	used := map[int]bool{}
	bindings := make([]AuthoredCurrentDetailMaskBinding, 0, len(refs))
	for _, ref := range refs {
		original, err := s.Original.ReadJSON(ctx, who, source.Tenant, source.Project, source.Parent, source.Instruction.Ordinal, ref)
		if err != nil {
			return nil, nil, err
		}
		masks, indices, err := compileAuthoredMaskPaths([]json.RawMessage{original}, currentImage, source.Source.Source, documents)
		if err != nil {
			return nil, nil, err
		}
		selected := make([]json.RawMessage, 0, len(indices))
		for _, i := range indices {
			witness, err := proofs[i].Witness()
			if err != nil {
				return nil, nil, err
			}
			selected = append(selected, witness)
			used[i] = true
		}
		transforms, err := canonical.Marshal(selected)
		if err != nil {
			return nil, nil, err
		}
		published, err := s.Masks.Publish(ctx, source.Tenant, source.Parent, source.Instruction.Ordinal, masks[0], ref, original, currentImage, transforms)
		if err != nil {
			return nil, nil, err
		}
		bindings = append(bindings, AuthoredCurrentDetailMaskBinding{Source: ref, Current: published})
	}
	indices := make([]int, 0, len(used))
	for i := range proofs {
		if used[i] {
			indices = append(indices, i)
		}
	}
	return bindings, indices, nil
}
