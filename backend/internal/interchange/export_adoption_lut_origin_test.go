package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerLUTAuthorityFailsBeforeIO(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if err := catalog.QualifyExportLUT(context.Background(), v2input.Entry{}, nil, v2input.Entry{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled LUT authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportLUT(ctx, v2input.Entry{}, nil, v2input.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled LUT authority: %v", err)
	}
}
