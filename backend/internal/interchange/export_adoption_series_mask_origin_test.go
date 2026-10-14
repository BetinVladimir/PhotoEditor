package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerSeriesMaskOriginFailsBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportSeriesMaskOrigin(context.Background(),
		ExportSeriesMeasureOrigin{}, v2input.Entry{}, nil,
		ExportClosureNode{}, v2input.Ref{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled mask origin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportSeriesMaskOrigin(ctx,
		ExportSeriesMeasureOrigin{}, v2input.Entry{}, nil,
		ExportClosureNode{}, v2input.Ref{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mask origin: %v", err)
	}
}
