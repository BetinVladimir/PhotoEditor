package v2render

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/v2input"
)

// Deterministic first-occurrence order; repeated operands share the same actual
// native path without dropping the original expression or rewriting its order.
func authoredDetailExpressionRefs(ctx context.Context, expression json.RawMessage) ([]v2input.Ref, error) {
	refs := []v2input.Ref{}
	seen := map[v2input.Ref]bool{}
	nodes := 0
	var walk func(json.RawMessage, int) error
	walk = func(raw json.RawMessage, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		nodes++
		if depth > 16 || nodes > 256 {
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
			if len(node) != 2 {
				return v2input.ErrInvalid
			}
			var ref v2input.Ref
			if decodeAuthoredParams(node["mask"], &ref) != nil || !validLayerArtifactRef(ref) {
				return v2input.ErrInvalid
			}
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		case "invert":
			if len(node) != 2 {
				return v2input.ErrInvalid
			}
			return walk(node["operand"], depth+1)
		case "union", "intersect", "subtract":
			if len(node) != 3 {
				return v2input.ErrInvalid
			}
			if err := walk(node["left"], depth+1); err != nil {
				return err
			}
			return walk(node["right"], depth+1)
		default:
			return v2input.ErrInvalid
		}
		return nil
	}
	if len(expression) == 0 || string(expression) == "null" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return refs, nil
	}
	if err := walk(expression, 1); err != nil {
		return nil, err
	}
	return refs, ctx.Err()
}
