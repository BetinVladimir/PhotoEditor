package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportEvidenceSaveFailsBeforeSQL(t *testing.T) {
	var prepared PreparedExportAdoption
	for _, operation := range []struct {
		name string
		call func(context.Context) error
	}{
		{"save", func(ctx context.Context) error {
			return prepared.SaveScheduler(ctx,
				SQLExportAdoptionClaimStore{}, ExportAdoptionClaim{}, FrozenExportDocuments{}, "photos")
		}},
		{"adopt", func(ctx context.Context) error {
			return prepared.AdoptScheduler(ctx,
				SQLExportAdoptionClaimStore{}, ExportAdoptionClaim{}, FrozenExportDocuments{}, "photos")
		}},
	} {
		if err := operation.call(context.Background()); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatalf("uninstalled %s: %v", operation.name, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := operation.call(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled %s: %v", operation.name, err)
		}
	}
}
