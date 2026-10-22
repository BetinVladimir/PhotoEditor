package semanticreview

import (
	"context"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

func TestPreviewResponseRejectsUnverifiedBeforeSigning(t *testing.T) {
	called := false
	sign := func(context.Context, objectstore.Object, time.Time) (string, error) { called = true; return "", nil }
	_, err := BuildExposurePreviewResponse(context.Background(), PhysicalExposurePreview{}, PreviewRequest{}, time.Now(), sign,
		func(context.Context) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("unverified preview reached signing")
	}
}
