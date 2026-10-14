package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestExportResultCorrectionSourceRequiresCanonicalInputCatalog(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("{}"))}
	entry := v2input.Entry{Ref: ref, Contract: "CorrectionPlan"}
	if _, err := (SQLExportResultInputCatalog{}).LookupExportCorrectionEvidence(
		context.Background(), entry, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("result correction source accepted missing canonical catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLExportResultInputCatalog{}).LookupExportCorrectionEvidence(ctx, entry, []byte("{}")); err != context.Canceled {
		t.Fatal("result correction source lost cancellation", err)
	}
}
