package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestSchedulerBodyDependencyRejectsMissingClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	parent := v2input.Entry{Contract: "Params", Ref: ref}
	child := v2input.Entry{Contract: "PoseSet", Ref: ref}
	expected := v2qualification.Expected{Operation: "pose.detect", Version: "1", LicenseCleared: true}
	if _, err := catalog.ReadExportBodyDependencyQualification(context.Background(), nil, ref, expected); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled report authority: %v", err)
	}
	if _, err := catalog.LookupExportBodyDependencyOrigin(context.Background(), parent, []byte("{}"), child); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled origin authority: %v", err)
	}
	if err := catalog.MatchExportBodyDependencyProfile(context.Background(), ExportBodyDependencyOrigin{}, parent, []byte("{}"), child, expected); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled profile authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.ReadExportBodyDependencyQualification(ctx, nil, ref, expected); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled report read: %v", err)
	}
	if _, err := catalog.LookupExportBodyDependencyOrigin(ctx, parent, []byte("{}"), child); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled origin read: %v", err)
	}
}

func TestSchedulerBodyDependencyInstallationRejectsMissingFrozenClaim(t *testing.T) {
	installation := SQLExportBodyDependencyInstallation{}
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if _, err := installation.BuildExportBodyDependencyQualifier(context.Background(),
		CreateIntent{}, FrozenExportDocuments{}, catalog, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("scheduler dependency without frozen claim: %v", err)
	}
}
