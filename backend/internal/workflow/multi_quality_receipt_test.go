package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2render"
	"testing"
)

// Receipt validator fixture exercises identities only; it does not establish
// compiler/content authority or substitute for database acceptance evidence.
func TestMultiQualityReceiptIdentityAndStates(t *testing.T) {
	run := uuid.New()
	result := uuid.New()
	plan := MultiQualityPlan{run: run, source: v2render.MultiPreviewSource{Binding: v2render.AdoptedMultiBinding{ResultID: result}}}
	value := MultiQualityReceipt{Run: run, SourceResult: result, Quality: uuid.NewSHA1(run, []byte("quality.check:master"))}
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled"} {
		value.State = state
		if err := validateMultiQualityReceipt(plan, value); err != nil {
			t.Fatal(state, err)
		}
	}
	for _, change := range []func(*MultiQualityReceipt){
		func(v *MultiQualityReceipt) { v.Run = uuid.New() },
		func(v *MultiQualityReceipt) { v.SourceResult = uuid.New() },
		func(v *MultiQualityReceipt) { v.Quality = uuid.New() },
		func(v *MultiQualityReceipt) { v.State = "draft" },
		func(v *MultiQualityReceipt) { v.State = "ready" },
	} {
		candidate := value
		change(&candidate)
		if err := validateMultiQualityReceipt(plan, candidate); err == nil {
			t.Fatal("foreign receipt accepted")
		}
	}
}
func TestMultiQualityCoordinatorPreflight(t *testing.T) {
	c := MultiQualityCoordinator{}
	if _, err := c.Prepare(context.Background(), uuid.New(), uuid.New(), "optical.panorama"); err == nil {
		t.Fatal("unconfigured coordinator accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Prepare(ctx, uuid.New(), uuid.New(), "optical.focus_stack"); err != context.Canceled {
		t.Fatal(err)
	}
	if _, _, err := (MultiQualityIntentStore{}).ReadReceipt(ctx, MultiQualityPlan{}); err != context.Canceled {
		t.Fatal(err)
	}
}
