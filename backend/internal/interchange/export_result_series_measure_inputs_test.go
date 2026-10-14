package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultSeriesMeasureInputsRequireCanonicalResult(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportSeriesMeasureInputs(context.Background(),
		ExportSeriesMeasureOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}); err != v2input.ErrInvalid {
		t.Fatal("measure inputs accepted without canonical root", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportSeriesMeasureInputs(ctx,
		ExportSeriesMeasureOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}); err != context.Canceled {
		t.Fatal("measure inputs lost cancellation", err)
	}
}
