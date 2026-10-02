// Package v2render compiles ordered editor layers without dropping nonparametric edits.
package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type Layer struct {
	ID               uuid.UUID       `json:"id"`
	Enabled          bool            `json:"enabled"`
	Opacity          float64         `json:"opacity"`
	Blend            string          `json:"blend"`
	Operation        string          `json:"operation"`
	OperationVersion string          `json:"operation_version"`
	Params           v2input.Ref     `json:"params"`
	Mask             json.RawMessage `json:"mask"`
}

func (l Layer) Active() bool { return l.Enabled && l.Opacity > 0 }

type MaskNode struct {
	SHA                  string
	Operation            string
	Ref                  *v2input.Ref
	Left, Right, Operand string
}
type PlannedLayer struct {
	Layer    Layer
	MaskRoot string
}
type Plan struct {
	Source v2input.SessionVersionBinding
	Layers []PlannedLayer
	Masks  []MaskNode
}

// Compile validates the source/canvas/prefix invariants first. Every layer is
// retained in recipe order; mask nodes are deduplicated by canonical expression
// hash and emitted after their dependencies. Artifact authority/byte verification
// remains the scoped resolver's responsibility before this pure compiler call.
func Compile(raw []byte, binding v2input.SessionVersionBinding) (Plan, error) {
	var empty Plan
	if _, err := v2draft.ValidateLayerStack(raw, binding); err != nil {
		return empty, err
	}
	var stack struct {
		Layers []Layer `json:"layers"`
	}
	if err := json.Unmarshal(raw, &stack); err != nil {
		return empty, err
	}
	plan := Plan{Source: binding, Layers: make([]PlannedLayer, 0, len(stack.Layers)), Masks: []MaskNode{}}
	seen := map[string]bool{}
	var walk func(json.RawMessage) (string, error)
	walk = func(raw json.RawMessage) (string, error) {
		body, err := canonical.Bytes(raw)
		if err != nil {
			return "", err
		}
		var expr struct {
			Op      string          `json:"op"`
			Mask    *v2input.Ref    `json:"mask"`
			Left    json.RawMessage `json:"left"`
			Right   json.RawMessage `json:"right"`
			Operand json.RawMessage `json:"operand"`
		}
		if err = json.Unmarshal(body, &expr); err != nil {
			return "", err
		}
		var value map[string]json.RawMessage
		if err = json.Unmarshal(body, &value); err != nil {
			return "", err
		}
		hash, err := canonical.Hash(value)
		if err != nil {
			return "", err
		}
		if seen[hash] {
			return hash, nil
		}
		node := MaskNode{SHA: hash, Operation: expr.Op}
		switch expr.Op {
		case "ref":
			node.Ref = expr.Mask
		case "invert":
			node.Operand, err = walk(expr.Operand)
		case "union", "intersect", "subtract":
			node.Left, err = walk(expr.Left)
			if err == nil {
				node.Right, err = walk(expr.Right)
			}
		default:
			return "", v2input.ErrInvalid
		}
		if err != nil {
			return "", err
		}
		seen[hash] = true
		plan.Masks = append(plan.Masks, node)
		return hash, nil
	}
	for _, layer := range stack.Layers {
		root, err := walk(layer.Mask)
		if err != nil {
			return empty, err
		}
		layer.Mask = append(json.RawMessage(nil), layer.Mask...)
		plan.Layers = append(plan.Layers, PlannedLayer{layer, root})
	}
	return plan, nil
}

// ActiveMasks returns the required mask subgraph in its compiled topological
// order. Inactive layers remain in Plan for history but require no raster IO.
func (p Plan) ActiveMasks() ([]MaskNode, error) {
	nodes := make(map[string]MaskNode, len(p.Masks))
	for _, node := range p.Masks {
		if node.SHA == "" {
			return nil, v2input.ErrInvalid
		}
		if _, exists := nodes[node.SHA]; exists {
			return nil, v2input.ErrInvalid
		}
		nodes[node.SHA] = node
	}
	needed := map[string]bool{}
	visiting := map[string]bool{}
	var visit func(string, int) error
	visit = func(hash string, depth int) error {
		if depth > 16 || visiting[hash] {
			return v2input.ErrInvalid
		}
		if needed[hash] {
			return nil
		}
		node, exists := nodes[hash]
		if !exists {
			return v2input.ErrInvalid
		}
		visiting[hash] = true
		for _, child := range []string{node.Left, node.Right, node.Operand} {
			if child != "" {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		delete(visiting, hash)
		needed[hash] = true
		return nil
	}
	for _, layer := range p.Layers {
		if layer.Layer.Active() {
			if err := visit(layer.MaskRoot, 0); err != nil {
				return nil, err
			}
		}
	}
	result := []MaskNode{}
	emitted := map[string]bool{}
	for _, node := range p.Masks {
		if needed[node.SHA] {
			for _, child := range []string{node.Left, node.Right, node.Operand} {
				if child != "" && !emitted[child] {
					return nil, v2input.ErrInvalid
				}
			}
			result = append(result, node)
			emitted[node.SHA] = true
		}
	}
	return result, nil
}
