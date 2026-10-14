package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportEvidenceSaveFailsBeforeSQL(t *testing.T) {
	var prepared PreparedExportAdoption
	if err := prepared.SaveScheduler(context.Background(), SQLExportAdoptionClaimStore{},
		ExportAdoptionClaim{}, FrozenExportDocuments{}, "photos"); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled save: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := prepared.SaveScheduler(ctx, SQLExportAdoptionClaimStore{},
		ExportAdoptionClaim{}, FrozenExportDocuments{}, "photos"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled save: %v", err)
	}
}
