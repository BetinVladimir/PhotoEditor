package semanticproposal

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestNativeTransferQualificationSelectionCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	if configured, err := reader.prepareNativeTransferRuntimeConfiguration(ctx, auth.Identity{}, uuid.New(), TransferPreparation{}); err != context.Canceled || configured.Qualification != (v2input.SemanticQualificationPin{}) {
		t.Fatal("cancelled runtime configuration exposed qualification", err)
	}
	if pin, err := reader.discoverNativeTransferQualificationPin(ctx, auth.Identity{}, uuid.New(), v2qualification.Expected{}); err != context.Canceled || pin != (v2input.SemanticQualificationPin{}) {
		t.Fatal("cancelled pin discovery reached IO", err)
	}
	if selected, err := reader.selectNativeTransferQualificationPolicy(ctx, auth.Identity{}, uuid.New(), 16<<20); err != context.Canceled || selected.pin != (v2input.SemanticQualificationPin{}) {
		t.Fatal("cancelled policy selection exposed pin", err)
	}
	for _, budget := range []int64{0, -1, 9007199254740992} {
		if selected, err := reader.selectNativeTransferQualificationPolicy(context.Background(), auth.Identity{}, uuid.New(), budget); err == nil || selected.pin != (v2input.SemanticQualificationPin{}) {
			t.Fatal("invalid budget exposed qualification")
		}
	}
}
