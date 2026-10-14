package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportPersonOriginFactoryFailsBeforeIO(t *testing.T) {
	factory := SQLSchedulerExportPersonOriginInstallation{Bucket: "photos"}
	if _, err := factory.BuildSchedulerExportPersonOrigins(context.Background(),
		ExportAdoptionClaim{}, SchedulerExportResult{}, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled factory: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := factory.BuildSchedulerExportPersonOrigins(ctx,
		ExportAdoptionClaim{}, SchedulerExportResult{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled factory: %v", err)
	}
}

func TestSchedulerExportPersonOriginInventoryMustStayPinned(t *testing.T) {
	input := exportPersonOriginStoredInput{ref: v2input.Ref{ArtifactID: uuid.New(),
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		object: objectstore.Object{Key: "receipt", VersionID: "v1", Bytes: 10,
			ContentType: "application/json", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if !sameSchedulerPersonOriginInputs([]exportPersonOriginStoredInput{input}, []exportPersonOriginStoredInput{input}) {
		t.Fatal("identical inventory rejected")
	}
	changed := input
	changed.object.VersionID = "v2"
	if sameSchedulerPersonOriginInputs([]exportPersonOriginStoredInput{input}, []exportPersonOriginStoredInput{changed}) ||
		sameSchedulerPersonOriginInputs([]exportPersonOriginStoredInput{input}, nil) {
		t.Fatal("changed inventory accepted")
	}
}
