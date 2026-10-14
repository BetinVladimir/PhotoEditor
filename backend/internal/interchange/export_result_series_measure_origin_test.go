package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultSeriesMeasureOriginRequiresCanonicalSource(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportSeriesMeasureOrigin(context.Background(),
		ExportSeriesOriginEvidence{}, v2input.Entry{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("measure origin accepted without canonical source", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportSeriesMeasureOrigin(ctx,
		ExportSeriesOriginEvidence{}, v2input.Entry{}, []byte("{}")); err != context.Canceled {
		t.Fatal("measure origin lost cancellation", err)
	}
}
