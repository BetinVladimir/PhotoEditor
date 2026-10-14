package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultDenseWarpRequiresCanonicalInputs(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportDenseWarpOccurrence(context.Background(),
		v2input.Entry{}, nil, 0, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("warp accepted without canonical parent and field")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportDenseWarpOccurrence(ctx,
		v2input.Entry{}, nil, 0, v2input.Entry{}); err != context.Canceled {
		t.Fatal("warp lost cancellation", err)
	}
}
