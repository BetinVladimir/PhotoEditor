package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultSeriesOriginRequiresCanonicalResult(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportSeriesOrigin(context.Background(), v2input.Entry{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("series origin accepted without canonical catalog", err)
	}
	if _, err := catalog.LookupExportSeriesOriginDocument(context.Background(), ExportSeriesOriginEvidence{}, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("series stats accepted without canonical root", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportSeriesOrigin(ctx, v2input.Entry{}, []byte("{}")); err != context.Canceled {
		t.Fatal("series origin lost cancellation", err)
	}
}
