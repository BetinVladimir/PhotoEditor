package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestSchedulerPromptPolicyRequiresInstalledClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	expected := v2qualification.Expected{Operation: "mask.prompt", Version: "1", LicenseCleared: true}
	if _, err := catalog.ReadExportPromptQualification(context.Background(), nil, ref, expected); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled report authority: %v", err)
	}
	if err := catalog.QualifyExportPromptProfile(context.Background(), ExportSeriesMaskOrigin{}, v2input.SessionVersionBinding{}, expected); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled profile authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.ReadExportPromptQualification(ctx, nil, ref, expected); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled report read: %v", err)
	}
	if err := catalog.QualifyExportPromptProfile(ctx, ExportSeriesMaskOrigin{}, v2input.SessionVersionBinding{}, expected); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled profile read: %v", err)
	}
}

func TestSchedulerPromptInstallationRejectsMissingFrozenClaim(t *testing.T) {
	installation := SQLExportPromptMaskPolicyInstallation{}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := installation.BuildExportPromptMaskPolicy(context.Background(),
		CreateIntent{}, FrozenExportDocuments{}, catalog, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("scheduler policy without frozen claim: %v", err)
	}
}
