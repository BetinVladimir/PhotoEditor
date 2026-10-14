package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerSeriesOriginFailsBeforeSQL(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	entry := v2input.Entry{Contract: "SeriesCorrections", Ref: ref}
	if _, err := catalog.LookupExportSeriesOrigin(context.Background(), entry, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled origin: %v", err)
	}
	if _, err := catalog.LookupExportSeriesOriginDocument(context.Background(), ExportSeriesOriginEvidence{},
		v2input.Entry{Contract: "SeriesStats", Ref: ref}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled stat origin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportSeriesOrigin(ctx, entry, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled origin: %v", err)
	}
	if _, err := catalog.LookupExportSeriesOriginDocument(ctx, ExportSeriesOriginEvidence{},
		v2input.Entry{Contract: "SeriesStats", Ref: ref}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled stat origin: %v", err)
	}
}
