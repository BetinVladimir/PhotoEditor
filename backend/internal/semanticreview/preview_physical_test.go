package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPhysicalPreviewRequiresAuthorityBeforeIO(t *testing.T) {
	got, err := ReadPhysicalExposurePreview(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), previewParamsFixture(), []byte("{}"), "", "")
	if err == nil || got.verified {
		t.Fatal("unverified preview escaped")
	}
}

func TestPreviewPNGReaderCancellationBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if readAndVerifyPreviewPNG(ctx, nil, "", PlanSource{}, 1, 1, "", "before") != context.Canceled {
		t.Fatal("cancellation ignored")
	}
}
