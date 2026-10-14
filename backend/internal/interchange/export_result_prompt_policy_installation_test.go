package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestExportResultPromptPolicyRejectsMissingScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportPromptProfile(context.Background(), ExportSeriesMaskOrigin{}, v2input.SessionVersionBinding{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("prompt profile accepted without scoped database", err)
	}
	if _, err := catalog.ReadExportPromptQualification(context.Background(), nil, v2input.Ref{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("prompt qualification accepted without pinned report", err)
	}
}
