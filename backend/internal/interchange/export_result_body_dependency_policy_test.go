package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestExportResultBodyDependencyPolicyRequiresScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.ReadExportBodyDependencyQualification(context.Background(), nil, v2input.Ref{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("qualification accepted without configured scope", err)
	}
	if err := catalog.MatchExportBodyDependencyProfile(context.Background(), ExportBodyDependencyOrigin{}, v2input.Entry{}, nil, v2input.Entry{}, v2qualification.Expected{}); err != v2input.ErrInvalid {
		t.Fatal("profile accepted without canonical origin", err)
	}
}
