package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultBodyFitInventoryRequiresCanonicalScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportBodyFitInputInventory(context.Background(), v2input.Entry{}, nil, ExportClosureNode{}); err != v2input.ErrInvalid {
		t.Fatal("body-fit inventory accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportBodyFitInputInventory(ctx, v2input.Entry{}, nil, ExportClosureNode{}); err != context.Canceled {
		t.Fatal("body-fit inventory lost cancellation", err)
	}
}
