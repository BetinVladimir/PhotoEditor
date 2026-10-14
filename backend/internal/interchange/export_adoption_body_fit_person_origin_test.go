package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyFitPersonOriginRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportBodyFitPersonOrigin(context.Background(),
		v2input.Entry{}, nil, ExportClosureNode{}, "before", ExportClosureNode{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled paired person origin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyFitPersonOrigin(ctx,
		v2input.Entry{}, nil, ExportClosureNode{}, "after", ExportClosureNode{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled paired person origin: %v", err)
	}
}
