package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestSchedulerBodyDetectorProfileRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if err := catalog.MatchExportBodyFitDetectorProfile(context.Background(),
		ExportBodyDependencyOrigin{}, ExportBodyDependencyOrigin{},
		v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, nil,
		v2input.Entry{}, v2qualification.Expected{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled detector profile authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.MatchExportBodyFitDetectorProfile(ctx,
		ExportBodyDependencyOrigin{}, ExportBodyDependencyOrigin{},
		v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, nil,
		v2input.Entry{}, v2qualification.Expected{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled detector profile authority: %v", err)
	}
}
