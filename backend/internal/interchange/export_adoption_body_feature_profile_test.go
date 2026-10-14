package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestSchedulerBodyFeatureProfileRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if err := catalog.MatchExportBodyFitFeatureProfile(context.Background(),
		ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{},
		v2input.Entry{}, v2qualification.Expected{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled feature profile authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.MatchExportBodyFitFeatureProfile(ctx,
		ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{},
		v2input.Entry{}, v2qualification.Expected{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled feature profile authority: %v", err)
	}
}
