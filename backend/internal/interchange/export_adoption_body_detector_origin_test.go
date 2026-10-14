package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyDetectorOriginRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.LookupExportBodyFitDetectorOrigin(context.Background(),
		ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{},
		v2input.Entry{}, nil, v2input.Entry{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled detector origin authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyFitDetectorOrigin(ctx,
		ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{},
		v2input.Entry{}, nil, v2input.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled detector origin authority: %v", err)
	}
}
