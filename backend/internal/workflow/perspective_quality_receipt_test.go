package workflow

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestPerspectiveQualityReceiptExactTasksAndState(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		t.Fatal(err)
	}
	value := PerspectiveQualityReceipt{Run: plan.run, SourceResult: plan.source.ResultID, State: "running", Quality: plan.task}
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled"} {
		value.State = state
		if err := validatePerspectiveQualityReceipt(plan, value); err != nil {
			t.Fatal("terminal observation refused", state, err)
		}
	}
	for _, mutate := range []func(*PerspectiveQualityReceipt){func(v *PerspectiveQualityReceipt) { v.Run = uuid.New() }, func(v *PerspectiveQualityReceipt) { v.SourceResult = uuid.New() }, func(v *PerspectiveQualityReceipt) { v.Quality = uuid.New() }, func(v *PerspectiveQualityReceipt) { v.State = "ready" }, func(v *PerspectiveQualityReceipt) { v.State = "draft" }} {
		bad := value
		mutate(&bad)
		if err := validatePerspectiveQualityReceipt(plan, bad); err == nil {
			t.Fatal("foreign/invalid acknowledgement accepted")
		}
	}
	if _, _, err := (PerspectiveQualityIntentStore{}).ReadReceipt(context.Background(), plan); err == nil {
		t.Fatal("missing DB accepted")
	}
}
func TestPerspectiveQualityCoordinatorRefusesBeforeIO(t *testing.T) {
	c := PerspectiveQualityCoordinator{}
	if _, err := c.Prepare(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("unconfigured coordinator accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Prepare(ctx, uuid.New(), uuid.New()); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
