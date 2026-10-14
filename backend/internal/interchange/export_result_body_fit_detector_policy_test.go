package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestExportResultBodyFitDetectorPolicyRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.ReadExportBodyFitDetectorQualification(context.Background(), nil, v2input.Ref{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("detector qualification accepted without scope", err)
	}
	if err := catalog.MatchExportBodyFitDetectorProfile(context.Background(), ExportBodyDependencyOrigin{}, ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, ExportClosureNode{}, v2input.Entry{}, nil, v2input.Entry{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("detector profile accepted without scope", err)
	}
}
