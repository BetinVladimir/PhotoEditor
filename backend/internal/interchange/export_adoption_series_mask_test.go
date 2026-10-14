package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerSeriesMaskFailsBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportSeriesMeasureMask(context.Background(),
		ExportSeriesMeasureOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Ref{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled mask source: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportSeriesMeasureMask(ctx,
		ExportSeriesMeasureOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Ref{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mask source: %v", err)
	}
}
