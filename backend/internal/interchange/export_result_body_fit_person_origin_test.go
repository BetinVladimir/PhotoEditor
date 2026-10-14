package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultBodyFitPersonOriginRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportBodyFitPersonOrigin(context.Background(), v2input.Entry{}, nil, ExportClosureNode{}, "before", ExportClosureNode{}); err != v2input.ErrInvalid {
		t.Fatal("person origin accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyFitPersonOrigin(ctx, v2input.Entry{}, nil, ExportClosureNode{}, "before", ExportClosureNode{}); err != context.Canceled {
		t.Fatal("person origin lost cancellation", err)
	}
}
