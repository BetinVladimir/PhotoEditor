package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerAlignmentBackgroundObjectRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.lookupExportAlignmentBackgroundObject(context.Background(),
		ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{},
		v2input.Entry{}, nil, ExportClosureNode{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled background object authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.lookupExportAlignmentBackgroundObject(ctx,
		ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{},
		v2input.Entry{}, nil, ExportClosureNode{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled background object authority: %v", err)
	}
}
