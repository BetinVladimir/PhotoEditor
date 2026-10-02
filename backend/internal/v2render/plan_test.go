package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestCompilePreservesLayerOrderAndReusesMaskDAG(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{uuid.New(), strings.Repeat("a", 64)} }
	binding := v2input.SessionVersionBinding{uuid.New(), ref(), strings.Repeat("b", 64)}
	a, b := ref(), ref()
	leaf := func(ref v2input.Ref) map[string]any {
		return map[string]any{"op": "ref", "mask": ref, "canvas_hash": binding.CanvasHash}
	}
	expression := map[string]any{"op": "subtract", "left": map[string]any{"op": "union", "left": leaf(a), "right": leaf(b)}, "right": map[string]any{"op": "invert", "operand": leaf(b)}}
	id1, id2 := uuid.New(), uuid.New()
	params1, params2 := ref(), ref()
	layers := []any{
		map[string]any{"id": id1, "enabled": true, "opacity": 1, "blend": "normal", "operation": "optical.lens", "operation_version": "1", "params": params1, "mask": expression},
		map[string]any{"id": id2, "enabled": false, "opacity": .5, "blend": "screen", "operation": "adjust.exposure", "operation_version": "1", "params": params2, "mask": expression},
	}
	raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": layers})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Compile(raw, binding)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Layers) != 2 || len(plan.Masks) != 5 || plan.Source != binding {
		t.Fatal("lost layers or duplicate expressions", plan)
	}
	if plan.Layers[0].Layer.ID != id1 || plan.Layers[1].Layer.ID != id2 || plan.Layers[0].Layer.Params != params1 || plan.Layers[1].Layer.Params != params2 || !plan.Layers[0].Layer.Active() || plan.Layers[1].Layer.Active() || plan.Layers[1].Layer.Blend != "screen" {
		t.Fatal("layer metadata/order lost")
	}
	if plan.Layers[0].MaskRoot != plan.Layers[1].MaskRoot {
		t.Fatal("equivalent masks not reused")
	}
	emitted := map[string]bool{}
	for _, node := range plan.Masks {
		for _, dependency := range []string{node.Left, node.Right, node.Operand} {
			if dependency != "" && !emitted[dependency] {
				t.Fatal("mask node before dependency")
			}
		}
		emitted[node.SHA] = true
	}
	budget, err := plan.BudgetLayer(id1, 257, 2, &a, 512)
	wantBudget, _ := LayerResources(257, 2, 2, 5, 512)
	if err != nil || budget.MaskRefs != 2 || len(budget.Masks) != 5 || budget.Resources != wantBudget {
		t.Fatal("compiled mask resource mismatch", budget, err)
	}
	if _, err := plan.BudgetLayer(id2, 257, 2, nil, 512); err == nil {
		t.Fatal("inactive layer received composition budget")
	}
	second, err := Compile(raw, binding)
	if err != nil {
		t.Fatal(err)
	}
	firstSHA, _ := canonical.Hash(plan)
	secondSHA, _ := canonical.Hash(second)
	if firstSHA != secondSHA {
		t.Fatal("plan compilation not deterministic")
	}
	before := string(plan.Layers[0].Layer.Mask)
	for i := range raw {
		raw[i] = ' '
	}
	if string(plan.Layers[0].Layer.Mask) != before {
		t.Fatal("compiled plan retained caller document bytes")
	}
}

func TestCompileRejectsInvalidWholeStack(t *testing.T) {
	binding := v2input.SessionVersionBinding{uuid.New(), v2input.Ref{uuid.New(), strings.Repeat("a", 64)}, strings.Repeat("b", 64)}
	for _, raw := range [][]byte{[]byte(`{}`), []byte(`{"schema_version":1,"source":null,"layers":[]}`)} {
		if plan, err := Compile(raw, binding); err == nil || plan.Layers != nil || plan.Masks != nil {
			t.Fatal("invalid stack produced partial plan")
		}
	}
	raw, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{}})
	plan, err := Compile(raw, binding)
	if err != nil || len(plan.Layers) != 0 || len(plan.Masks) != 0 {
		t.Fatal("empty stack", err)
	}
}

func TestActiveMasksPrunesInactiveAndRefusesBrokenDAG(t *testing.T) {
	p := Plan{Masks: []MaskNode{{SHA: "a", Operation: "ref"}, {SHA: "b", Operation: "ref"}, {SHA: "c", Operation: "invert", Operand: "a"}}, Layers: []PlannedLayer{{Layer: Layer{Enabled: true, Opacity: 1}, MaskRoot: "c"}, {Layer: Layer{Enabled: false, Opacity: 1}, MaskRoot: "b"}}}
	active, err := p.ActiveMasks()
	if err != nil || len(active) != 2 || active[0].SHA != "a" || active[1].SHA != "c" {
		t.Fatal("inactive mask not pruned", active, err)
	}
	p.Layers[0].Layer.Opacity = 0
	if active, err = p.ActiveMasks(); err != nil || len(active) != 0 {
		t.Fatal("zero opacity still rasterizes masks", err)
	}
	p.Layers[0].Layer.Opacity = 1
	p.Masks[2].Operand = "missing"
	if _, err = p.ActiveMasks(); err == nil {
		t.Fatal("missing dependency accepted")
	}
	p.Masks[2].Operand = "c"
	if _, err = p.ActiveMasks(); err == nil {
		t.Fatal("cyclic mask graph accepted")
	}
	p.Masks[2].Operand = "a"
	p.Masks[0], p.Masks[2] = p.Masks[2], p.Masks[0]
	if _, err = p.ActiveMasks(); err == nil {
		t.Fatal("non-topological mask graph accepted")
	}
}
