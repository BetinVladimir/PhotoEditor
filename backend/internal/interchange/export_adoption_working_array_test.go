package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerWorkingArrayAuthorityFailsBeforeIO(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if err := catalog.QualifyExportWorkingArray(context.Background(), v2input.Entry{}, v2input.Entry{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportWorkingArray(ctx, v2input.Entry{}, v2input.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled authority: %v", err)
	}
}
