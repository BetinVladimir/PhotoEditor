package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultSeriesRequiresCanonicalInputCatalog(t *testing.T) {
	if err := (SQLExportResultInputCatalog{}).QualifyExportSeries(
		context.Background(), v2input.Entry{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("series accepted without canonical result catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SQLExportResultInputCatalog{}).QualifyExportSeries(
		ctx, v2input.Entry{}, []byte("{}")); err != context.Canceled {
		t.Fatal("series lost cancellation", err)
	}
}
