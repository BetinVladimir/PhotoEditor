package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyFeatureOriginRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportBodyFitFeatureOrigin(context.Background(),
		v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled feature origin authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyFitFeatureOrigin(ctx,
		v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled feature origin authority: %v", err)
	}
}
