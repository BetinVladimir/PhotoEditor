package v2input

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

type captureProgressFake struct {
	p   *CapturePreparationProgress
	err error
}

func (f captureProgressFake) PreparationProgress(context.Context, CaptureAssessmentLease) (*CapturePreparationProgress, error) {
	return f.p, f.err
}

type captureStarterFake struct {
	calls    int
	progress *CapturePreparationProgress
}

func (f *captureStarterFake) StartCapturePreparation(_ context.Context, _ CaptureAssessmentLease, p *CapturePreparationProgress) error {
	f.calls++
	f.progress = p
	return nil
}
func TestCapturePipelineRoutesDurablePhases(t *testing.T) {
	for _, phase := range []string{"absent", "draft", "pending", "ready", "failed", "cancelled", "unknown"} {
		t.Run(phase, func(t *testing.T) {
			run := uuid.New()
			workflow, _ := WorkflowID(run)
			l := CaptureAssessmentLease{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), ProjectID: uuid.New(), ReceiptID: uuid.New(), SessionID: uuid.New(), AssetVersionID: uuid.New(), Until: time.Now().Add(time.Minute), CreatedAt: time.Now(), Attempts: 1}
			var progress *CapturePreparationProgress
			if phase != "absent" {
				l.PreparationWorkflowID = &workflow
				progress = &CapturePreparationProgress{workflow, run, phase}
			}
			starter := &captureStarterFake{}
			assessments := 0
			assessment := captureAdmissionFunc(func(context.Context, CaptureAssessmentLease) error { assessments++; return nil })
			pipeline := CapturePipeline{captureProgressFake{p: progress}, starter, assessment}
			err := pipeline.AdmitCapture(context.Background(), l)
			switch phase {
			case "absent", "draft":
				if !errors.Is(err, ErrCapturePending) || starter.calls != 1 || starter.progress != progress {
					t.Fatalf("%v %+v", err, starter)
				}
			case "pending":
				if !errors.Is(err, ErrCapturePending) || starter.calls != 0 {
					t.Fatal(err)
				}
			case "ready":
				if err != nil || assessments != 1 {
					t.Fatal(err)
				}
			case "failed", "cancelled":
				var permanent CapturePermanentFailure
				if !errors.As(err, &permanent) {
					t.Fatal(err)
				}
			default:
				if !errors.Is(err, ErrInvalid) {
					t.Fatal(err)
				}
			}
			if phase != "ready" && assessments != 0 {
				t.Fatal("assessment ran before adoption")
			}
		})
	}
}
