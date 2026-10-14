package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultBodyFitInstallationRejectsIncompleteScope(t *testing.T) {
	installation := SQLExportBodyFitInstallation{}
	if _, err := installation.BuildExportBodyFitQualifier(context.Background(), CreateIntent{}, FrozenExportDocuments{}, SQLExportResultInputCatalog{}, nil, nil, nil, nil, nil); err != v2input.ErrInvalid {
		t.Fatal("incomplete result body-fit installed", err)
	}
}
