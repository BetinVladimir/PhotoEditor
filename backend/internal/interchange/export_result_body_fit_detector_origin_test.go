package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultBodyFitDetectorOriginRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportBodyFitDetectorOrigin(context.Background(), ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, nil, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("detector origin accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyFitDetectorOrigin(ctx, ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, nil, v2input.Entry{}); err != context.Canceled {
		t.Fatal("detector origin lost cancellation", err)
	}
}
