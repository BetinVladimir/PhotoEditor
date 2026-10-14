package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultBodyFitFeatureInstallationRejectsIncompleteScope(t *testing.T) {
	installation := SQLExportBodyFitFeatureInstallation{}
	if _, err := installation.BuildExportBodyFitFeatureQualifier(context.Background(), CreateIntent{}, FrozenExportDocuments{}, SQLExportResultInputCatalog{}, nil, nil, nil, nil); err != v2input.ErrInvalid {
		t.Fatal("incomplete result feature qualifier installed", err)
	}
}
