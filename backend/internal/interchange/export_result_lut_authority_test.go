package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultLUTRequiresCanonicalScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportLUT(context.Background(), v2input.Entry{}, nil, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("LUT accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportLUT(ctx, v2input.Entry{}, nil, v2input.Entry{}); err != context.Canceled {
		t.Fatal("LUT lost cancellation", err)
	}
}
