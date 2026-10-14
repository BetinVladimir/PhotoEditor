package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestSchedulerPersonPolicyRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	expected := v2qualification.Expected{Operation: "mask.person", Version: "1", LicenseCleared: true}
	if _, err := catalog.ReadExportPersonQualification(context.Background(), nil, ref, expected); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled report authority: %v", err)
	}
	if err := catalog.QualifyExportPersonProfile(context.Background(), ExportStatsPersonOrigin{}, v2input.SessionVersionBinding{}, expected); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled profile authority: %v", err)
	}
	if _, err := NewSchedulerExportStatsPersonPolicy(catalog, nil, nil, ref, expected); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled policy: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.ReadExportPersonQualification(ctx, nil, ref, expected); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled report read: %v", err)
	}
	if err := catalog.QualifyExportPersonProfile(ctx, ExportStatsPersonOrigin{}, v2input.SessionVersionBinding{}, expected); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled profile read: %v", err)
	}
}

func TestSchedulerPersonInstallationRejectsMissingFrozenClaim(t *testing.T) {
	installation := SQLExportStatsPersonPolicyInstallation{}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := installation.BuildExportStatsPersonPolicy(context.Background(),
		CreateIntent{}, FrozenExportDocuments{}, catalog, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("scheduler policy without frozen claim: %v", err)
	}
}
