package semanticreview

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
)

type retouchInstallationObjects struct{ forbiddenExposureObjects }

func (s *retouchInstallationObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	s.calls++
	return nil, ErrInvalid
}

// Constructor coverage only: zero-value pools must never be queried here.
// Live role verification belongs to InstallExposureApplyService, not this test.
func TestShownRetouchServiceCompositionRequiresPreviewAndCopyBudget(t *testing.T) {
	entry := cleanupTenantCatalogFixture().Tenants[0]
	raw, err := os.ReadFile("../../../resources/icc/work_rec2020_gamma24_v1.icc")
	if err != nil {
		t.Fatal(err)
	}
	color, err := NewExposureColorProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	objects := &retouchInstallationObjects{}
	api, admission := &pgxpool.Pool{}, &pgxpool.Pool{}
	provider, err := NewRetouchPreviewProvider(api, objects, "owned", func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (RetouchPreviewLookup, error) {
		t.Fatal("constructor resolved physical preview")
		return RetouchPreviewLookup{}, ErrInvalid
	}, func(context.Context, objectstore.Object, time.Time) (string, error) {
		t.Fatal("constructor signed a preview")
		return "", ErrInvalid
	}, entry.Review.ModelSHA, entry.Review.DatasetSHA)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"disabled", "enabled", "missing_preview", "invalid_copy_pin", "invalid_copy_vram"} {
		t.Run(mode, func(t *testing.T) {
			copyBudget := entry.Execution
			copyBudget.Qualification.Report.ArtifactID = uuid.New()
			copyBudget.Qualification.Calibration.ArtifactID = uuid.New()
			config := ExposureApplyInstallation{API: api, Admission: admission, Objects: objects, Bucket: "owned", Color: color,
				DocumentBudgetBytes: entry.Review.DocumentBudgetBytes, ReviewModelSHA: entry.Review.ModelSHA,
				ReviewDatasetSHA: entry.Review.DatasetSHA, Execution: entry.Execution}
			if mode != "disabled" {
				config.ShownRetouchExecution = &copyBudget
				config.ShownRetouchPreview = provider
			}
			switch mode {
			case "missing_preview":
				config.ShownRetouchPreview = nil
			case "invalid_copy_pin":
				copyBudget.Qualification.Report.ArtifactID = uuid.Nil
			case "invalid_copy_vram":
				copyBudget.Limits.VRAMBytes = 1
			}
			service, err := NewExposureApplyService(config)
			valid := mode == "disabled" || mode == "enabled"
			if valid {
				if err != nil || service == nil || service.Prepare == nil || service.API != api || service.Admission != admission {
					t.Fatal("valid service composition rejected", err)
				}
			} else if err == nil || service != nil {
				t.Fatal("incomplete shown retouch composition accepted")
			}
			if objects.calls != 0 {
				t.Fatal("service composition performed object IO")
			}
		})
	}
}
