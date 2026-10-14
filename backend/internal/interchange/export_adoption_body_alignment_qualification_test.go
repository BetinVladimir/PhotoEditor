package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestSchedulerAlignmentQualificationRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := catalog.ReadExportBodyFitAlignmentQualification(context.Background(),
		nil, v2input.Ref{}, v2qualification.Expected{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled registration policy authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.ReadExportBodyFitAlignmentQualification(ctx,
		nil, v2input.Ref{}, v2qualification.Expected{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled registration policy authority: %v", err)
	}
}
