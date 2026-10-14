package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultBodyFitFeatureOriginRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportBodyFitFeatureOrigin(context.Background(), v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("feature origin accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyFitFeatureOrigin(ctx, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}); err != context.Canceled {
		t.Fatal("feature origin lost cancellation", err)
	}
}
