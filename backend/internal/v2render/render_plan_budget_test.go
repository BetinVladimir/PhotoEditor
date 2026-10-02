package v2render

import (
	"github.com/google/uuid"
	"testing"
)

func TestRenderPlanBudgetCountsSharedNodesPerActiveLayer(t *testing.T) {
	p, _, ref := budgetPlan()
	p.Layers[1].MaskRoot = p.Layers[0].MaskRoot
	got, err := p.BudgetRender(257, 2, &ref, 512, "require_float32", RenderProviderPeak{VRAMBytes: 123})
	want, _ := RenderResources(257, 2, 1, 5, 512, "require_float32", RenderProviderPeak{VRAMBytes: 123})
	if err != nil || got.MaskRefs != 1 || got.MaskNodes != 5 || got.Resources != want {
		t.Fatal(got, err)
	}
	p.Layers[1].Layer.Enabled = false
	got, err = p.BudgetRender(257, 2, nil, 512, "require_float32", RenderProviderPeak{})
	if err != nil || got.MaskRefs != 1 || got.MaskNodes != 2 {
		t.Fatal(got, err)
	}
	p.Layers[1].Layer.Enabled = true
	p.Layers[1].Layer.ID = uuid.New()
	p.Layers[1].MaskRoot = "missing"
	if _, err = p.BudgetRender(257, 2, nil, 512, "require_float32", RenderProviderPeak{}); err == nil {
		t.Fatal("broken mask graph admitted")
	}
}
