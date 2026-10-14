package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportPersonOriginCurrentCheckerFailsClosed(t *testing.T) {
	checker := SchedulerExportPersonOriginCurrentChecker{}
	if err := checker.RecheckExportPersonOrigin(context.Background(), PortablePersonOriginClaim{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled checker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checker.RecheckExportPersonOrigin(ctx, PortablePersonOriginClaim{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled checker: %v", err)
	}
}
