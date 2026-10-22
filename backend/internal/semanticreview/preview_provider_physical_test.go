package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPhysicalPreviewProviderCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var provider *ExposurePreviewProvider
	physical, selection, err := provider.ReadPhysicalPreview(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, PreviewRequest{})
	if err != context.Canceled || physical.verified || selection.PreviewRunID != uuid.Nil {
		t.Fatal("cancelled physical read admitted", err)
	}
}
