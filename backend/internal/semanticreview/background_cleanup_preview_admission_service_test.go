package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestBackgroundCleanupPreviewAdmissionServiceCancellationBeforePreparation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var service *BackgroundCleanupPreviewAdmissionService
	got, err := service.AdmitPreview(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), PreviewRequest{})
	if err != context.Canceled || got.RunID != uuid.Nil {
		t.Fatal("cancelled admission escaped", err)
	}
}

func TestBackgroundCleanupPreviewAdmissionServiceRequiresInstalledPreparation(t *testing.T) {
	params := previewParamsFixture()
	request := PreviewRequest{ExpectedRevision: params.WorkflowRevision, TargetRevision: params.TargetRevision, PlanSHA: params.Plan.SHA256, CandidateSHA: params.Candidate.SHA256}
	var service *BackgroundCleanupPreviewAdmissionService
	got, err := service.AdmitPreview(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, params.TenantID, params.IntentID, params.TargetID, request)
	if err != ErrUnavailable || got.RunID != uuid.Nil {
		t.Fatal("uninstalled admission returned run", err)
	}
}

func TestBackgroundCleanupPreviewAdmissionRejectsInvalidBudgetBeforeIO(t *testing.T) {
	for _, mode := range []string{"ram", "cuda_cap", "metal_budget"} {
		t.Run(mode, func(t *testing.T) {
			config := cleanupTenantCatalogFixture().Tenants[0].BackgroundCleanupPreview
			switch mode {
			case "ram":
				config.Limits.RAMBytes = 1
			case "cuda_cap":
				config.Qualification.Backend = "cuda"
				config.MeasuredDeviceBytes = 10000000001
				config.Limits.VRAMBytes = config.MeasuredDeviceBytes
			case "metal_budget":
				config.Qualification.Backend = "mps"
				config.MeasuredDeviceBytes = 12000000000
				config.Limits.VRAMBytes = 11999999999
			}
			objects := &forbiddenExposureObjects{}
			reads := 0
			service := &BackgroundCleanupPreviewAdmissionService{DB: &pgxpool.Pool{}, Objects: objects, Reader: func(context.Context, PlanSource) ([]byte, error) {
				reads++
				t.Fatal("invalid installation reached object reader")
				return nil, ErrInvalid
			}, Bucket: "photos", ReviewBudget: 64 << 20, ReviewModelSHA: config.Qualification.ModelLockSHA, ReviewDatasetSHA: config.Qualification.DatasetSHA, OwnerCosts: config.OwnerCosts, Limits: config.Limits, Qualification: config.Qualification, MeasuredDeviceBytes: config.MeasuredDeviceBytes, ContextPixels: config.ContextPixels, FeatherPixels: config.FeatherPixels}
			params := previewParamsFixture()
			request := PreviewRequest{ExpectedRevision: params.WorkflowRevision, TargetRevision: params.TargetRevision, PlanSHA: params.Plan.SHA256, CandidateSHA: params.Candidate.SHA256}
			got, err := service.AdmitPreview(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, params.TenantID, params.IntentID, params.TargetID, request)
			if err != ErrUnavailable || got.RunID != uuid.Nil || reads != 0 || objects.calls != 0 {
				t.Fatal("invalid installation escaped pre-IO rejection", got, err, reads, objects.calls)
			}
		})
	}
}
