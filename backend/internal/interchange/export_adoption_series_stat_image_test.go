package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerSeriesStatImageFailsBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	entry := v2input.Entry{Contract: "SeriesStats", Ref: v2input.Ref{
		ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if err := catalog.QualifyExportSeriesStatImage(context.Background(), ExportSeriesOriginEvidence{}, entry, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled stat/image authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportSeriesStatImage(ctx, ExportSeriesOriginEvidence{}, entry, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled stat/image authority: %v", err)
	}
}
