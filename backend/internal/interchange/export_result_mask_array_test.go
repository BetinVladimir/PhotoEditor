package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultMaskArrayRequiresCanonicalScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportMaskArray(context.Background(), v2input.Entry{}, nil, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("mask array accepted without scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportMaskArray(ctx, v2input.Entry{}, nil, v2input.Entry{}); err != context.Canceled {
		t.Fatal("mask array lost cancellation", err)
	}
}
