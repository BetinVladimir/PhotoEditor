package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestExportResultPersonOriginRequiresCanonicalAuthority(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportStatsPersonOrigin(context.Background(),
		v2input.Entry{}, v2input.Ref{}, nil, review.WorkingImage{}); err != v2input.ErrInvalid {
		t.Fatal("person origin accepted without canonical input")
	}
	if _, err := catalog.LookupExportStatsPersonDocument(context.Background(),
		ExportStatsPersonOrigin{}, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("person document accepted without canonical root")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportStatsPersonDocument(ctx,
		ExportStatsPersonOrigin{}, v2input.Entry{}); err != context.Canceled {
		t.Fatal("person document lost cancellation", err)
	}
}
