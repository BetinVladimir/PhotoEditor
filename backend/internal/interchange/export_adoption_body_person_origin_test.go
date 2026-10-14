package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyPersonOriginRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportBodyPersonOrigin(context.Background(),
		v2input.Entry{}, nil, ExportClosureNode{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled geometry person origin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyPersonOrigin(ctx,
		v2input.Entry{}, nil, ExportClosureNode{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled geometry person origin: %v", err)
	}
}
