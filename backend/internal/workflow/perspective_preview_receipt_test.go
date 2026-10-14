package workflow

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestPerspectivePreviewReceiptExactTasksAndState(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectivePreviewPlan(bundle, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	value := PerspectivePreviewReceipt{Run: plan.run, SourceResult: plan.source.ResultID, State: "running", Preview1600: plan.documents[0].ID, Preview2560: plan.documents[1].ID}
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled"} {
		value.State = state
		if err := validatePerspectivePreviewReceipt(plan, value); err != nil {
			t.Fatal("terminal observation refused", state, err)
		}
	}
	for _, mutate := range []func(*PerspectivePreviewReceipt){func(v *PerspectivePreviewReceipt) { v.Run = uuid.New() }, func(v *PerspectivePreviewReceipt) { v.SourceResult = uuid.New() }, func(v *PerspectivePreviewReceipt) { v.Preview1600 = v.Preview2560 }, func(v *PerspectivePreviewReceipt) { v.State = "ready" }, func(v *PerspectivePreviewReceipt) { v.State = "draft" }} {
		bad := value
		mutate(&bad)
		if err := validatePerspectivePreviewReceipt(plan, bad); err == nil {
			t.Fatal("foreign/invalid acknowledgement accepted")
		}
	}
	if _, _, err := (PerspectivePreviewIntentStore{}).ReadReceipt(context.Background(), plan); err == nil {
		t.Fatal("missing DB accepted")
	}
}
func TestPerspectivePreviewCoordinatorRefusesBeforeIO(t *testing.T) {
	c := PerspectivePreviewCoordinator{}
	if _, err := c.Prepare(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("unconfigured coordinator accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Prepare(ctx, uuid.New(), uuid.New()); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
