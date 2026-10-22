package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPresentationLinkageCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := ReadExposurePreviewPresentationLinkage(ctx, nil, nil, "", auth.Identity{}, uuid.New(), previewParamsFixture(), "", "")
	if err != context.Canceled || got.Document.TaskID != uuid.Nil {
		t.Fatal("cancelled linkage escaped", err)
	}
}

func TestPresentationLinkageMissingAuthorityBeforeIO(t *testing.T) {
	got, err := ReadExposurePreviewPresentationLinkage(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), previewParamsFixture(), "", "")
	if err == nil || got.Document.TaskID != uuid.Nil {
		t.Fatal("unauthorized linkage escaped")
	}
}
