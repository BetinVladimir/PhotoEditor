package workflow

import (
	"context"
	"testing"
)

func TestMultiQualityIntentRefusesUnverifiedAndCancelled(t *testing.T) {
	if _, err := (MultiQualityIntentStore{}).Save(context.Background(), MultiQualityPlan{}); err == nil {
		t.Fatal("unverified intent saved")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (MultiQualityIntentStore{}).Save(ctx, MultiQualityPlan{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
