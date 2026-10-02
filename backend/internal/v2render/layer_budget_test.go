package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func budgetPlan() (Plan, uuid.UUID, v2input.Ref) {
	id := uuid.New()
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	other := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	return Plan{Layers: []PlannedLayer{{Layer: Layer{ID: id, Enabled: true, Opacity: 1}, MaskRoot: "invert"}, {Layer: Layer{ID: uuid.New(), Enabled: true, Opacity: 1}, MaskRoot: "other"}}, Masks: []MaskNode{{SHA: "leaf", Operation: "ref", Ref: &ref}, {SHA: "invert", Operation: "invert", Operand: "leaf"}, {SHA: "other", Operation: "ref", Ref: &other}}}, id, ref
}

func TestBudgetLayerReachabilityProtectionDedupAndOwnership(t *testing.T) {
	plan, id, ref := budgetPlan()
	got, err := plan.BudgetLayer(id, 257, 2, &ref, 512)
	want, _ := LayerResources(257, 2, 1, 2, 512)
	if err != nil || got.MaskRefs != 1 || len(got.Masks) != 2 || got.Masks[1].SHA != "invert" || got.Resources != want {
		t.Fatalf("budget %+v %v", got, err)
	}
	got.Masks[0].Ref.SHA256 = strings.Repeat("c", 64)
	if plan.Masks[0].Ref.SHA256 != ref.SHA256 {
		t.Fatal("budget aliases input refs")
	}
	protected := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	other, err := plan.BudgetLayer(id, 257, 2, &protected, 512)
	want, _ = LayerResources(257, 2, 2, 2, 512)
	if err != nil || other.MaskRefs != 2 || other.Resources != want {
		t.Fatal("protection not budgeted", other, err)
	}
}

func TestBudgetLayerRejectsInactiveMissingMalformedGraphAndProtection(t *testing.T) {
	cases := []func(*Plan, uuid.UUID){
		func(p *Plan, id uuid.UUID) { p.Layers[0].Layer.Enabled = false },
		func(p *Plan, id uuid.UUID) { p.Layers[0].Layer.Opacity = 0 },
		func(p *Plan, id uuid.UUID) { p.Layers[0].MaskRoot = "missing" },
		func(p *Plan, id uuid.UUID) { p.Layers = append(p.Layers, p.Layers[0]) },
		func(p *Plan, id uuid.UUID) { p.Masks[0].Ref = nil },
		func(p *Plan, id uuid.UUID) { p.Masks[1].Operation = "unknown" },
		func(p *Plan, id uuid.UUID) { p.Masks[1].Operand = "invert" },
		func(p *Plan, id uuid.UUID) { p.Masks[0], p.Masks[1] = p.Masks[1], p.Masks[0] },
	}
	for i, mutate := range cases {
		p, id, _ := budgetPlan()
		mutate(&p, id)
		if _, err := p.BudgetLayer(id, 2, 1, nil, 512); err == nil {
			t.Fatalf("accepted case %d", i)
		}
	}
	p, id, _ := budgetPlan()
	for _, bad := range []v2input.Ref{{}, {ArtifactID: uuid.New(), SHA256: strings.Repeat("A", 64)}} {
		if _, err := p.BudgetLayer(id, 2, 1, &bad, 512); err == nil {
			t.Fatal("invalid protection")
		}
	}
	if _, err := p.BudgetLayer(uuid.New(), 2, 1, nil, 512); err == nil {
		t.Fatal("missing layer")
	}
}
