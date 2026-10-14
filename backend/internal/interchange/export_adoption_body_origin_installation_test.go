package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerBodyOriginInstallationRejectsMissingFrozenClaim(t *testing.T) {
	installation := SQLExportBodyOriginInstallation{}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := installation.BuildExportBodyOriginValidator(context.Background(),
		CreateIntent{}, FrozenExportDocuments{}, catalog, nil, nil, nil, nil, nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("scheduler geometry.body without frozen claim: %v", err)
	}
}
