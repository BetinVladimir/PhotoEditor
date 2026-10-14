package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultAlignmentBackgroundOriginRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportAlignmentBackgroundOrigin(context.Background(), ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, nil, ExportClosureNode{}); err != v2input.ErrInvalid {
		t.Fatal("background origin accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportAlignmentBackgroundOrigin(ctx, ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, nil, ExportClosureNode{}); err != context.Canceled {
		t.Fatal("background origin lost cancellation", err)
	}
}
