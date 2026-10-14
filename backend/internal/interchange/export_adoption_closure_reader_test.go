package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportClosureReaderFailsBeforeObjectIO(t *testing.T) {
	reader := SchedulerExportClosureReader{}
	if _, _, err := reader.Prepare(context.Background(), ExportAdoptionClaim{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled closure reader: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := reader.Prepare(ctx, ExportAdoptionClaim{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled closure reader: %v", err)
	}
}

func TestSchedulerExportEvidenceRequiresOriginalClaim(t *testing.T) {
	if _, err := freezeSchedulerExportAdoptionEvidence(context.Background(),
		ExportAdoptionClaim{}, "photos", SchedulerExportResult{}, VerifiedExportArchive{}, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("unclaimed evidence: %v", err)
	}
}
