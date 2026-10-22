package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
 "strings"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)

func TestRemovalPreviewAdmissionServiceCancellationBeforePreparation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var service *RemovalPreviewAdmissionService
	got, err := service.AdmitPreview(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), PreviewRequest{})
	if err != context.Canceled || got.RunID != uuid.Nil {
		t.Fatal("cancelled admission escaped", err)
	}
}

func TestRemovalPreviewAdmissionServiceRequiresInstalledPreparation(t *testing.T) {
	params := previewParamsFixture()
	request := PreviewRequest{ExpectedRevision: params.WorkflowRevision, TargetRevision: params.TargetRevision, PlanSHA: params.Plan.SHA256, CandidateSHA: params.Candidate.SHA256}
	var service *RemovalPreviewAdmissionService
	got, err := service.AdmitPreview(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, params.TenantID, params.IntentID, params.TargetID, request)
	if err != ErrUnavailable || got.RunID != uuid.Nil {
		t.Fatal("uninstalled admission returned run", err)
	}
}


func TestRemovalAdmissionRejectsInvalidDeploymentBeforeDatabaseAndObjects(t *testing.T) {
	params := previewParamsFixture()
	request := PreviewRequest{ExpectedRevision:params.WorkflowRevision, TargetRevision:params.TargetRevision, PlanSHA:params.Plan.SHA256, CandidateSHA:params.Candidate.SHA256}
	sha := strings.Repeat("a",64)
	for _, mutation := range []string{"ram_owner","index_owner","ram_limit","pixel_limit","device_zero","cuda_cap","policy","report_alias","runtime"} {
		t.Run(mutation,func(t *testing.T) {
			objects := &forbiddenExposureObjects{}
			pin := RemovalQualificationPin{PolicyID:uuid.New(),QualificationID:uuid.New(),IssuerID:uuid.New(),PolicySHA:sha,Backend:"cuda",Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Calibration:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},ModelLockSHA:sha,RuntimeLockSHA:sha,DeviceProfileSHA:sha,DatasetSHA:sha}
			service := &RemovalPreviewAdmissionService{DB:&pgxpool.Pool{},Objects:objects,
				Reader:func(context.Context,PlanSource)([]byte,error){t.Fatal("invalid deployment reached object reader");return nil,ErrInvalid},
				Bucket:"fixture",ReviewBudget:1048576,ReviewModelSHA:sha,ReviewDatasetSHA:sha,
				OwnerCosts:ExposureOwnerCosts{RuntimeRAMBytes:1048576,InputIndexScratchBytes:16*1048576},
				Limits:ExposureResourceEstimate{RAMBytes:1<<40,VRAMBytes:1<<40,ScratchBytes:1<<40,OutputBytes:1<<40,PixelCount:200000000},
				Qualification:pin,MeasuredDeviceBytes:10000000000,ContextPixels:128,FeatherPixels:8}
			switch mutation {
			case "ram_owner":service.OwnerCosts.RuntimeRAMBytes=0
			case "index_owner":service.OwnerCosts.InputIndexScratchBytes=1
			case "ram_limit":service.Limits.RAMBytes=1
			case "pixel_limit":service.Limits.PixelCount=200000001
			case "device_zero":service.MeasuredDeviceBytes=0
			case "cuda_cap":service.MeasuredDeviceBytes=10000000001
			case "policy":service.Qualification.PolicyID=uuid.Nil
			case "report_alias":service.Qualification.Calibration=service.Qualification.Report
			case "runtime":service.Qualification.RuntimeLockSHA="bad"
			}
			got,err := service.AdmitPreview(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},params.TenantID,params.IntentID,params.TargetID,request)
			if err!=ErrUnavailable || got.RunID!=uuid.Nil || got.TaskID!=uuid.Nil || got.CommitAttempted || objects.calls!=0 {t.Fatal("invalid deployment escaped",got,err,objects.calls)}
		})
	}
}
