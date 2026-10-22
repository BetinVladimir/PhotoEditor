package semanticreview

import (
	"github.com/google/uuid"
	"testing"
)

func TestRetouchShownRouteRequiresExactSelectedFamily(t *testing.T) {
	request := ApplyDecision{TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: "approve"}}}
	// An unpublished retouch task cannot enter the historical rerender branch.
	if _, err := preparedApplyUsesShown(request, PreparedApply{ShownRetouchTasks: []PreparedShownRetouch{{}}}); err != ErrInvalid {
		t.Fatal("unselected retouch task accepted", err)
	}
}

func TestRetouchShownRouteRejectsMixedOrIncompletePreparation(t *testing.T) {
	request := ApplyDecision{TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: "approve", Preview: &PreviewSelection{}}}}
	valid := PreparedApply{ShownRetouchTasks: make([]PreparedShownRetouch, 1)}
	if shown, err := preparedApplyUsesShown(request, valid); err != nil || !shown {
		t.Fatal("own retouch route rejected", shown, err)
	}
	for _, mode := range []string{"exposure", "removal", "cleanup", "rerender", "missing", "extra", "partial_selection", "rejection"} {
		t.Run(mode, func(t *testing.T) {
			prepared := valid
			owned := ApplyDecision{TargetDecisions: append([]TargetDecision(nil), request.TargetDecisions...)}
			switch mode {
			case "exposure":
				prepared.ShownTasks = make([]PreparedShownExposure, 1)
			case "removal":
				prepared.ShownRemovalTasks = make([]PreparedShownRemoval, 1)
			case "cleanup":
				prepared.ShownBackgroundCleanupTasks = make([]PreparedShownBackgroundCleanup, 1)
			case "rerender":
				prepared.Tasks = make([]PreparedExposureTask, 1)
			case "missing":
				prepared.ShownRetouchTasks = nil
			case "extra":
				prepared.ShownRetouchTasks = make([]PreparedShownRetouch, 2)
			case "partial_selection":
				owned.TargetDecisions = append(owned.TargetDecisions, TargetDecision{TargetID: uuid.New(), Decision: "approve"})
			case "rejection":
				owned.TargetDecisions[0].Decision = "reject"
				owned.TargetDecisions[0].Preview = nil
			}
			if _, err := preparedApplyUsesShown(owned, prepared); err == nil {
				t.Fatal("mixed or incomplete preparation admitted")
			}
		})
	}
}
