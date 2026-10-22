package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestBackgroundCleanupPresentationLinkageCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := ReadBackgroundCleanupPreviewPresentationLinkage(ctx, nil, nil, "", auth.Identity{}, uuid.New(), BackgroundCleanupPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "background_cleanup", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}, "", "")
	if err != context.Canceled || got.Document.TaskID != uuid.Nil {
		t.Fatal("cancelled linkage escaped", err)
	}
}

func TestBackgroundCleanupPresentationLinkageMissingAuthorityBeforeIO(t *testing.T) {
	got, err := ReadBackgroundCleanupPreviewPresentationLinkage(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), BackgroundCleanupPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "background_cleanup", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}, "", "")
	if err == nil || got.Document.TaskID != uuid.Nil {
		t.Fatal("unauthorized linkage escaped")
	}
}
