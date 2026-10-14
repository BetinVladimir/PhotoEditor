package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerAlignmentInstallationRejectsMissingFrozenClaim(t *testing.T) {
	installation := SQLExportAlignmentInstallation{}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := installation.BuildExportAlignmentQualifier(context.Background(),
		CreateIntent{}, FrozenExportDocuments{}, catalog, nil, nil, nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("scheduler alignment without frozen claim: %v", err)
	}
}
