package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultDetectorInstallationRejectsIncompleteScope(t *testing.T) {
	installation := SQLExportDetectorInstallation{}
	if _, err := installation.BuildExportDetectorQualifier(context.Background(), CreateIntent{}, FrozenExportDocuments{}, SQLExportResultInputCatalog{}, nil, nil, nil, nil); err != v2input.ErrInvalid {
		t.Fatal("incomplete result detector installed", err)
	}
}
