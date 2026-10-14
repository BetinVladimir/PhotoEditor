package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyAlignmentOriginRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportBodyFitAlignmentOrigin(context.Background(),
		v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled alignment origin authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyFitAlignmentOrigin(ctx,
		v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled alignment origin authority: %v", err)
	}
}
