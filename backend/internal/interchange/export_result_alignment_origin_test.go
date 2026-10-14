package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultAlignmentOriginRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportBodyFitAlignmentOrigin(context.Background(), v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("alignment origin accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyFitAlignmentOrigin(ctx, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}); err != context.Canceled {
		t.Fatal("alignment origin lost cancellation", err)
	}
}
