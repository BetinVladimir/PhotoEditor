package workflow

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

// Identity/state validator only; a synthetic plan is not source admission proof.
func TestMultiPreviewReceiptBindsRunResultTasksAndState(t *testing.T) {
	plan := MultiPreviewPlan{run: uuid.New()}
	plan.source.Binding.ResultID = uuid.New()
	value := MultiPreviewReceipt{Run: plan.run, SourceResult: plan.source.Binding.ResultID, Preview1600: uuid.NewSHA1(plan.run, []byte("render.derivative:preview1600")), Preview2560: uuid.NewSHA1(plan.run, []byte("render.derivative:preview2560"))}
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled"} {
		value.State = state
		if err := validateMultiPreviewReceipt(plan, value); err != nil {
			t.Fatal("valid state refused", state, err)
		}
	}
	for _, kind := range []string{"run", "result", "swap", "draft", "ready"} {
		bad := value
		switch kind {
		case "run":
			bad.Run = uuid.New()
		case "result":
			bad.SourceResult = uuid.New()
		case "swap":
			bad.Preview1600, bad.Preview2560 = bad.Preview2560, bad.Preview1600
		default:
			bad.State = kind
		}
		if err := validateMultiPreviewReceipt(plan, bad); err == nil {
			t.Fatal("receipt substitution accepted", kind)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (MultiPreviewIntentStore{}).ReadReceipt(ctx, plan); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
	if _, _, err := (MultiPreviewIntentStore{}).ReadReceipt(context.Background(), MultiPreviewPlan{}); err == nil {
		t.Fatal("unverified plan accepted")
	}
}
