package workflow

import (
	"context"
	"photoeditor/backend/internal/v2render"
	"strings"
	"testing"
)

func TestPerspectivePreviewSealRefusesMissingOriginalVerification(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectivePreviewPlan(bundle, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err = SealPerspectivePreview(context.Background(), nil, plan, bundle, v2render.VerifiedPerspectivePreviewInputs{}); err == nil {
		t.Fatal("missing original verification admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = SealPerspectivePreview(ctx, nil, plan, bundle, v2render.VerifiedPerspectivePreviewInputs{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
