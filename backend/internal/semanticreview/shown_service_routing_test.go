package semanticreview

import (
 "testing"
 "github.com/google/uuid"
)

func TestShownApplyCannotFallBackToRerender(t *testing.T) {
	request := ApplyDecision{TargetDecisions: []TargetDecision{{Decision: "approve", Preview: &PreviewSelection{}}}}
	if _, err := preparedApplyUsesShown(request, PreparedApply{}); err == nil {
		t.Fatal("missing shown task admitted")
	}
	if _, err := preparedApplyUsesShown(request, PreparedApply{ShownTasks: make([]PreparedShownExposure, 1), Tasks: make([]PreparedExposureTask, 1)}); err == nil {
		t.Fatal("mixed render path admitted")
	}
	shown, err := preparedApplyUsesShown(request, PreparedApply{ShownTasks: make([]PreparedShownExposure, 1)})
	if err != nil || !shown {
		t.Fatal("shown route missing", err)
	}
}

func TestUnselectedApplyCannotInjectShownTasks(t *testing.T) {
	request := ApplyDecision{TargetDecisions: []TargetDecision{{Decision: "approve"}}}
	if _, err := preparedApplyUsesShown(request, PreparedApply{ShownTasks: make([]PreparedShownExposure, 1)}); err == nil {
		t.Fatal("unsolicited copy task admitted")
	}
	request.TargetDecisions = append(request.TargetDecisions, TargetDecision{Decision: "approve", Preview: &PreviewSelection{}})
	if _, err := preparedApplyUsesShown(request, PreparedApply{ShownTasks: make([]PreparedShownExposure, 2)}); err == nil {
		t.Fatal("partial preview selection admitted")
	}
}

func TestShownRemovalApplyRoutesWithoutExposureRerender(t *testing.T) {
	request := ApplyDecision{TargetDecisions: []TargetDecision{{Decision: "approve", Preview: &PreviewSelection{}}}}
	shown, err := preparedApplyUsesShown(request, PreparedApply{ShownRemovalTasks: make([]PreparedShownRemoval, 1)})
	if err != nil || !shown {
		t.Fatalf("shown=%v error=%v", shown, err)
	}
	for _, prepared := range []PreparedApply{
		{ShownRemovalTasks: make([]PreparedShownRemoval, 2)},
		{ShownRemovalTasks: make([]PreparedShownRemoval, 1), Tasks: make([]PreparedExposureTask, 1)},
	} {
		if _, err := preparedApplyUsesShown(request, prepared); err == nil {
			t.Fatal("inconsistent removal preparation admitted")
		}
	}
	request.TargetDecisions = append(request.TargetDecisions, TargetDecision{Decision: "approve", Preview: &PreviewSelection{}})
	if _, err := preparedApplyUsesShown(request, PreparedApply{ShownRemovalTasks: make([]PreparedShownRemoval, 1), ShownTasks: make([]PreparedShownExposure, 1)}); err == nil {
		t.Fatal("mixed batch silently routed to one operation")
	}
	request.TargetDecisions[0].Preview = nil
	request.TargetDecisions = request.TargetDecisions[:1]
	if _, err := preparedApplyUsesShown(request, PreparedApply{ShownRemovalTasks: make([]PreparedShownRemoval, 1)}); err == nil {
		t.Fatal("unselected removal injected")
	}
}

func TestShownBackgroundCleanupApplyRoutesWithoutExposureRerender(t *testing.T) {
	request := ApplyDecision{TargetDecisions: []TargetDecision{{Decision: "approve", Preview: &PreviewSelection{}}}}
	shown, err := preparedApplyUsesShown(request, PreparedApply{ShownBackgroundCleanupTasks: make([]PreparedShownBackgroundCleanup, 1)})
	if err != nil || !shown {
		t.Fatalf("shown=%v error=%v", shown, err)
	}
	for _, prepared := range []PreparedApply{
		{ShownBackgroundCleanupTasks: make([]PreparedShownBackgroundCleanup, 2)},
		{ShownBackgroundCleanupTasks: make([]PreparedShownBackgroundCleanup, 1), Tasks: make([]PreparedExposureTask, 1)},
	} {
		if _, err := preparedApplyUsesShown(request, prepared); err == nil {
			t.Fatal("inconsistent cleanup preparation admitted")
		}
	}
	request.TargetDecisions = append(request.TargetDecisions, TargetDecision{Decision: "approve", Preview: &PreviewSelection{}})
	if _, err := preparedApplyUsesShown(request, PreparedApply{ShownBackgroundCleanupTasks: make([]PreparedShownBackgroundCleanup, 1), ShownTasks: make([]PreparedShownExposure, 1)}); err == nil {
		t.Fatal("mixed batch silently routed to one operation")
	}
	request.TargetDecisions[0].Preview = nil
	request.TargetDecisions = request.TargetDecisions[:1]
	if _, err := preparedApplyUsesShown(request, PreparedApply{ShownBackgroundCleanupTasks: make([]PreparedShownBackgroundCleanup, 1)}); err == nil {
		t.Fatal("unselected cleanup injected")
	}
}

func TestShownCleanupAndRemovalCannotDropOneFamily(t *testing.T) {
	request := ApplyDecision{TargetDecisions: []TargetDecision{{Decision: "approve", Preview: &PreviewSelection{}}, {Decision: "approve", Preview: &PreviewSelection{}}}}
	_, err := preparedApplyUsesShown(request, PreparedApply{ShownBackgroundCleanupTasks: make([]PreparedShownBackgroundCleanup, 1), ShownRemovalTasks: make([]PreparedShownRemoval, 1)})
	if err == nil {
		t.Fatal("mixed cleanup/removal silently dropped one family")
	}
}


func TestRejectOnlyPreparationCannotCarryExecution(t *testing.T) {
 request := ApplyDecision{TargetDecisions: []TargetDecision{{Decision: "reject"}}}
 for _, mode := range []string{"valid", "child", "legacy", "shown_exposure", "shown_removal", "shown_cleanup", "shown_retouch"} {
  t.Run(mode, func(t *testing.T) {
   prepared := PreparedApply{}
   switch mode {
   case "child": prepared.Child = uuid.New()
   case "legacy": prepared.Tasks = []PreparedExposureTask{{}}
   case "shown_exposure": prepared.ShownTasks = []PreparedShownExposure{{}}
   case "shown_removal": prepared.ShownRemovalTasks = []PreparedShownRemoval{{}}
   case "shown_cleanup": prepared.ShownBackgroundCleanupTasks = []PreparedShownBackgroundCleanup{{}}
   case "shown_retouch": prepared.ShownRetouchTasks = []PreparedShownRetouch{{}}
   }
   shown, err := preparedApplyUsesShown(request, prepared)
   if mode == "valid" {
    if err != nil || shown { t.Fatal("plain rejection refused", err) }
   } else if err != ErrInvalid || shown { t.Fatal("execution silently dropped", mode, err) }
  })
 }
}
