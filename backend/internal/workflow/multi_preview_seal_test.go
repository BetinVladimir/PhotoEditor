package workflow

import (
	"context"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func TestMultiPreviewSealRejectsUnverifiedAndCancelledInput(t *testing.T) {
	if err := SealMultiPreview(context.Background(), nil, MultiPreviewPlan{}, v2render.MultiResultContent{}); err == nil {
		t.Fatal("unverified seal reached database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SealMultiPreview(ctx, nil, MultiPreviewPlan{}, v2render.MultiResultContent{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
