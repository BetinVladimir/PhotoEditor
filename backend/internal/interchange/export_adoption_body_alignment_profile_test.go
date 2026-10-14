package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestSchedulerAlignmentProfileRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if err := catalog.MatchExportBodyFitAlignmentProfile(context.Background(),
		ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{},
		v2input.Entry{}, v2qualification.Expected{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled registration profile authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.MatchExportBodyFitAlignmentProfile(ctx,
		ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{},
		v2input.Entry{}, v2qualification.Expected{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled registration profile authority: %v", err)
	}
}
