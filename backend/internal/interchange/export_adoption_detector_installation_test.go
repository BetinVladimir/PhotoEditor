package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerDetectorInstallationRejectsMissingFrozenClaim(t *testing.T) {
	installation := SQLExportDetectorInstallation{}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := installation.BuildExportDetectorQualifier(context.Background(),
		CreateIntent{}, FrozenExportDocuments{}, catalog, nil, nil, nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("scheduler detector without frozen claim: %v", err)
	}
}
