package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestExportResultAlignmentPolicyRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.ReadExportBodyFitAlignmentQualification(context.Background(), nil, v2input.Ref{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("alignment qualification accepted without scope", err)
	}
	if err := catalog.MatchExportBodyFitAlignmentProfile(context.Background(), ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("alignment profile accepted without scope", err)
	}
}
