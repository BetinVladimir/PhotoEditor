package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRemovalPresentationLinkageCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := ReadRemovalPreviewPresentationLinkage(ctx, nil, nil, "", auth.Identity{}, uuid.New(), RemovalPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "remove_object", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}, "", "")
	if err != context.Canceled || got.Document.TaskID != uuid.Nil {
		t.Fatal("cancelled linkage escaped", err)
	}
}

func TestRemovalPresentationLinkageMissingAuthorityBeforeIO(t *testing.T) {
	got, err := ReadRemovalPreviewPresentationLinkage(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), RemovalPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "remove_object", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}, "", "")
	if err == nil || got.Document.TaskID != uuid.Nil {
		t.Fatal("unauthorized linkage escaped")
	}
}
