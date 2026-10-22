package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRetouchPreviewAdmissionServiceCancellationBeforePreparation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var service *RetouchPreviewAdmissionService
	got, err := service.AdmitPreview(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), PreviewRequest{})
	if err != context.Canceled || got.RunID != uuid.Nil {
		t.Fatal("cancelled admission escaped", err)
	}
}

func TestRetouchPreviewAdmissionServiceRequiresInstalledPreparation(t *testing.T) {
	params := previewParamsFixture()
	request := PreviewRequest{ExpectedRevision: params.WorkflowRevision, TargetRevision: params.TargetRevision, PlanSHA: params.Plan.SHA256, CandidateSHA: params.Candidate.SHA256}
	var service *RetouchPreviewAdmissionService
	got, err := service.AdmitPreview(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, params.TenantID, params.IntentID, params.TargetID, request)
	if err != ErrUnavailable || got.RunID != uuid.Nil {
		t.Fatal("uninstalled admission returned run", err)
	}
}
