package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerStatsPersonOriginFailsBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportStatsPersonOrigin(context.Background(),
		v2input.Entry{}, v2input.Ref{}, nil, review.WorkingImage{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled person origin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportStatsPersonOrigin(ctx,
		v2input.Entry{}, v2input.Ref{}, nil, review.WorkingImage{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled person origin: %v", err)
	}
}
