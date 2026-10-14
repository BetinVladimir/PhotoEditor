package workflow

import (
	"context"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func TestMultiQualitySealRejectsUnverifiedAndCancelledInput(t *testing.T) {
	if err := SealMultiQuality(context.Background(), nil, MultiQualityPlan{}, v2render.MultiResultContent{}); err == nil {
		t.Fatal("unverified seal reached database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SealMultiQuality(ctx, nil, MultiQualityPlan{}, v2render.MultiResultContent{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
