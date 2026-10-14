package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultSeriesStatImageRequiresCanonicalOrigin(t *testing.T) {
	if err := (SQLExportResultInputCatalog{}).QualifyExportSeriesStatImage(
		context.Background(), ExportSeriesOriginEvidence{}, v2input.Entry{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("series stat accepted without canonical origin", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SQLExportResultInputCatalog{}).QualifyExportSeriesStatImage(
		ctx, ExportSeriesOriginEvidence{}, v2input.Entry{}, []byte("{}")); err != context.Canceled {
		t.Fatal("series stat lost cancellation", err)
	}
}
