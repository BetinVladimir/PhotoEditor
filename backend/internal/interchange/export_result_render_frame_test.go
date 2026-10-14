package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestExportResultRenderFrameRequiresCanonicalCatalog(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportRenderFrame(context.Background(), v2draft.LayerRef{}, nil); err != v2input.ErrInvalid {
		t.Fatal("render frame accepted without canonical catalog", err)
	}
	if err := catalog.QualifyExportRenderFrameArray(context.Background(), v2draft.LayerRef{}); err != v2input.ErrInvalid {
		t.Fatal("render frame array accepted without canonical catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportRenderFrame(ctx, v2draft.LayerRef{}, nil); err != context.Canceled {
		t.Fatal("render frame lost cancellation", err)
	}
	if err := catalog.QualifyExportRenderFrameArray(ctx, v2draft.LayerRef{}); err != context.Canceled {
		t.Fatal("render frame array lost cancellation", err)
	}
}
