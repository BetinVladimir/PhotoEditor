package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultSeriesMaskOriginRequiresCanonicalSource(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportSeriesMaskOrigin(context.Background(),
		ExportSeriesMeasureOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Ref{}); err != v2input.ErrInvalid {
		t.Fatal("series mask origin accepted without canonical source", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportSeriesMaskOrigin(ctx,
		ExportSeriesMeasureOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Ref{}); err != context.Canceled {
		t.Fatal("series mask origin lost cancellation", err)
	}
}
