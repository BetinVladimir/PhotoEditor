package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerCorrectionMemberFailsBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if err := catalog.QualifyExportCorrectionMember(context.Background(),
		v2input.SessionVersionBinding{}, v2input.Ref{}, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled correction member authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportCorrectionMember(ctx,
		v2input.SessionVersionBinding{}, v2input.Ref{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled correction member authority: %v", err)
	}
}
