package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultCorrectionMemberRequiresCanonicalInputCatalog(t *testing.T) {
	if err := (SQLExportResultInputCatalog{}).QualifyExportCorrectionMember(context.Background(),
		v2input.SessionVersionBinding{}, v2input.Ref{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("correction member accepted without canonical result catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SQLExportResultInputCatalog{}).QualifyExportCorrectionMember(ctx,
		v2input.SessionVersionBinding{}, v2input.Ref{}, []byte("{}")); err != context.Canceled {
		t.Fatal("correction member lost cancellation", err)
	}
}
