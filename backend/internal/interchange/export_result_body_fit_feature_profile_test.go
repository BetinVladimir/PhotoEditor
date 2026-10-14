package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestExportResultBodyFitFeatureProfileRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.MatchExportBodyFitFeatureProfile(context.Background(), ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("feature profile accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.MatchExportBodyFitFeatureProfile(ctx, ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, v2qualification.Expected{}); err != context.Canceled {
		t.Fatal("feature profile lost cancellation", err)
	}
}
