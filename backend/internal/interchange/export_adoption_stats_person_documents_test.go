package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerStatsPersonDocumentFailsBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportStatsPersonDocument(context.Background(),
		ExportStatsPersonOrigin{}, v2input.Entry{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled person documents: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportStatsPersonDocument(ctx,
		ExportStatsPersonOrigin{}, v2input.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled person documents: %v", err)
	}
}
