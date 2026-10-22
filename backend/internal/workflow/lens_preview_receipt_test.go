package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func TestLensPreviewReceiptExactTasksAndState(t *testing.T) {
 run:=uuid.New()
 plan:=LensPreviewPlan{run:run,source:v2render.LensPreviewSource{ResultID:uuid.New()},documents:[]RenderDerivativeDocument{{ID:uuid.NewSHA1(run,[]byte("render.derivative:preview1600"))},{ID:uuid.NewSHA1(run,[]byte("render.derivative:preview2560"))}}}
	value := LensPreviewReceipt{Run: plan.run, SourceResult: plan.source.ResultID, State: "running", Preview1600: plan.documents[0].ID, Preview2560: plan.documents[1].ID}
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled"} {
		value.State = state
		if err := validateLensPreviewReceipt(plan, value); err != nil {
			t.Fatal("terminal observation refused", state, err)
		}
	}
	for _, mutate := range []func(*LensPreviewReceipt){func(v *LensPreviewReceipt) { v.Run = uuid.New() }, func(v *LensPreviewReceipt) { v.SourceResult = uuid.New() }, func(v *LensPreviewReceipt) { v.Preview1600 = v.Preview2560 }, func(v *LensPreviewReceipt) { v.State = "ready" }, func(v *LensPreviewReceipt) { v.State = "draft" }} {
		bad := value
		mutate(&bad)
		if err := validateLensPreviewReceipt(plan, bad); err == nil {
			t.Fatal("foreign/invalid acknowledgement accepted")
		}
	}
	if _, _, err := (LensPreviewIntentStore{}).ReadReceipt(context.Background(), plan); err == nil {
		t.Fatal("missing DB accepted")
	}
}
func TestLensPreviewCoordinatorRefusesBeforeIO(t *testing.T) {
	c := LensPreviewCoordinator{}
	if _, err := c.Prepare(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("unconfigured coordinator accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Prepare(ctx, uuid.New(), uuid.New()); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
