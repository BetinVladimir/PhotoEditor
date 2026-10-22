package semanticreview

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestShownRetouchApplyBudgetsRequireExactApprovedSet(t *testing.T) {
	fixture := fullShownRetouchContentFixture(t)
	first, second, rejected := uuid.New(), uuid.New(), uuid.New()
	request := ApplyDecision{TargetDecisions: []TargetDecision{
		{TargetID: first, Decision: "approve"},
		{TargetID: rejected, Decision: "reject"},
		{TargetID: second, Decision: "approve"},
	}}
	budget := ExposureInputBudget{TargetID: first, OwnerCosts: fixture.owner, Limits: fixture.limits, Qualification: ExposureQualificationPin(fixture.pin)}
	other := budget
	other.TargetID = second
	for _, order := range [][]ExposureInputBudget{{budget, other}, {other, budget}} {
		got, err := shownRetouchApplyBudgets(request, order)
		if err != nil || len(got) != 2 || got[first].TargetID != first || got[second].TargetID != second {
			t.Fatal("exact approved budget set rejected", err)
		}
		order[0].TargetID = uuid.New()
		if got[first].TargetID != first || got[second].TargetID != second {
			t.Fatal("budget map aliases selector slice")
		}
	}
	for _, mode := range []string{"missing", "extra", "duplicate", "foreign", "rejected", "nil_target", "vram", "cuda", "report", "no_approvals"} {
		t.Run(mode, func(t *testing.T) {
			selected := []ExposureInputBudget{budget, other}
			owned := request
			switch mode {
			case "missing":
				selected = selected[:1]
			case "extra":
				selected = append(selected, budget)
			case "duplicate":
				selected[1] = budget
			case "foreign":
				selected[1].TargetID = uuid.New()
			case "rejected":
				selected[1].TargetID = rejected
			case "nil_target":
				selected[1].TargetID = uuid.Nil
			case "vram":
				selected[1].Limits.VRAMBytes = 1
			case "cuda":
				selected[1].Qualification.Backend = "cuda"
			case "report":
				selected[1].Qualification.Report.ArtifactID = uuid.Nil
			case "no_approvals":
				owned.TargetDecisions = []TargetDecision{{TargetID: rejected, Decision: "reject"}}
				selected = nil
			}
			got, err := shownRetouchApplyBudgets(owned, selected)
			if !errors.Is(err, ErrInvalid) || got != nil {
				t.Fatal("invalid selection returned partial budget authority", err)
			}
		})
	}
}
