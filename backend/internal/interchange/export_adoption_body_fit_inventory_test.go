package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyFitInventoryRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	parent := v2input.Entry{Contract: "Params", Ref: ref}
	fit := ExportClosureNode{Entry: v2input.Entry{Contract: "BodyFit", Ref: ref}}
	if err := catalog.QualifyExportBodyFitInputInventory(context.Background(), parent, []byte("{}"), fit); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled fit inventory authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportBodyFitInputInventory(ctx, parent, []byte("{}"), fit); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fit inventory authority: %v", err)
	}
}
