package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerSeriesAuthorityFailsBeforeSQL(t *testing.T) {
	entry := v2input.Entry{Contract: "SeriesCorrections", Ref: v2input.Ref{
		ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if err := catalog.QualifyExportSeries(context.Background(), entry, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportSeries(ctx, entry, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled authority: %v", err)
	}
	entry.Contract = "WorkingImage"
	if err := catalog.QualifyExportSeries(context.Background(), entry, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("wrong contract: %v", err)
	}
}
