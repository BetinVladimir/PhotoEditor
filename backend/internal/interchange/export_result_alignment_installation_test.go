package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultAlignmentInstallationRejectsIncompleteScope(t *testing.T) {
	installation := SQLExportAlignmentInstallation{}
	if _, err := installation.BuildExportAlignmentQualifier(context.Background(), CreateIntent{}, FrozenExportDocuments{}, SQLExportResultInputCatalog{}, nil, nil, nil, nil); err != v2input.ErrInvalid {
		t.Fatal("incomplete result alignment installed", err)
	}
}
