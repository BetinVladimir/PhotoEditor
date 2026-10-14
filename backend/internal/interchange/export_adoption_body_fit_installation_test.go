package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyFitInstallationRejectsMissingFrozenClaim(t *testing.T) {
	installation := SQLExportBodyFitInstallation{}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := installation.BuildExportBodyFitQualifier(context.Background(),
		CreateIntent{}, FrozenExportDocuments{}, catalog, nil, nil, nil, nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("scheduler BodyFit without frozen claim: %v", err)
	}
}
