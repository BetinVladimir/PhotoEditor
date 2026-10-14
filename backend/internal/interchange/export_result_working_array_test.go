package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultWorkingArrayRequiresCanonicalScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportWorkingArray(context.Background(), v2input.Entry{}, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("array accepted without canonical scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportWorkingArray(ctx, v2input.Entry{}, v2input.Entry{}); err != context.Canceled {
		t.Fatal("array lost cancellation", err)
	}
}
