package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultDefectLabelRequiresCanonicalScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportDefectLabel(context.Background(), v2input.Entry{}, nil, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("defect label accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportDefectLabel(ctx, v2input.Entry{}, nil, v2input.Entry{}); err != context.Canceled {
		t.Fatal("defect label lost cancellation", err)
	}
}
