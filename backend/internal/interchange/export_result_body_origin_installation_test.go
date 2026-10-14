package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultBodyOriginInstallationRejectsIncompleteScope(t *testing.T) {
	installation := SQLExportBodyOriginInstallation{}
	if _, err := installation.BuildExportBodyOriginValidator(context.Background(), CreateIntent{}, FrozenExportDocuments{}, SQLExportResultInputCatalog{}, nil, nil, nil, nil, nil, nil); err != v2input.ErrInvalid {
		t.Fatal("incomplete result body-origin installed", err)
	}
}
