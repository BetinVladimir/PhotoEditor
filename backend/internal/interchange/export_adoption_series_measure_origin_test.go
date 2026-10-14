package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerSeriesMeasureOriginFailsBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	entry := v2input.Entry{Contract: "SeriesStats", Ref: v2input.Ref{
		ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if _, err := catalog.LookupExportSeriesMeasureOrigin(context.Background(),
		ExportSeriesOriginEvidence{}, entry, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled series measure origin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportSeriesMeasureOrigin(ctx,
		ExportSeriesOriginEvidence{}, entry, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled series measure origin: %v", err)
	}
}
