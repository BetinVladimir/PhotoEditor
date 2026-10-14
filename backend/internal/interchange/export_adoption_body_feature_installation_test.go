package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyFeatureInstallationRejectsMissingFrozenClaim(t *testing.T) {
	installation := SQLExportBodyFitFeatureInstallation{}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := installation.BuildExportBodyFitFeatureQualifier(context.Background(),
		CreateIntent{}, FrozenExportDocuments{}, catalog, nil, nil, nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("scheduler body features without frozen claim: %v", err)
	}
}
