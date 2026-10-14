package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerSeriesMeasureInputsFailBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	stat := v2input.Entry{Contract: "SeriesStats", Ref: v2input.Ref{
		ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if err := catalog.QualifyExportSeriesMeasureInputs(context.Background(),
		ExportSeriesMeasureOrigin{}, stat, nil, ExportClosureNode{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled input authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportSeriesMeasureInputs(ctx,
		ExportSeriesMeasureOrigin{}, stat, nil, ExportClosureNode{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled input authority: %v", err)
	}
}
