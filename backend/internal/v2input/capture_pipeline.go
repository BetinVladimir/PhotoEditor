package v2input

import "context"

type CaptureProgressReader interface {
	PreparationProgress(context.Context, CaptureAssessmentLease) (*CapturePreparationProgress, error)
}
type CapturePreparationStarter interface {
	// nil progress means no workflow has been durably bound. A draft progress
	// resumes publication against exactly its existing run/workflow IDs.
	StartCapturePreparation(context.Context, CaptureAssessmentLease, *CapturePreparationProgress) error
}

// CapturePipeline routes a fenced receipt through durable preparation and
// assessment. Successful preparation remains pending until worker adoption;
// only assessment admission can report final queue completion.
type CapturePipeline struct {
	Progress    CaptureProgressReader
	Preparation CapturePreparationStarter
	Assessment  CaptureAssessmentAdmission
}

func (p CapturePipeline) AdmitCapture(ctx context.Context, l CaptureAssessmentLease) error {
	if p.Progress == nil || p.Preparation == nil || p.Assessment == nil || !validCaptureLease(l) {
		return ErrInvalid
	}
	progress, err := p.Progress.PreparationProgress(ctx, l)
	if err != nil {
		return err
	}
	if progress == nil {
		if l.PreparationWorkflowID != nil {
			return ErrInvalid
		}
		if err = p.Preparation.StartCapturePreparation(ctx, l, nil); err != nil {
			return err
		}
		return ErrCapturePending
	}
	expected, err := WorkflowID(progress.RunID)
	if err != nil || expected != progress.WorkflowID || l.PreparationWorkflowID == nil || *l.PreparationWorkflowID != progress.WorkflowID {
		return ErrInvalid
	}
	switch progress.Phase {
	case "draft":
		if err = p.Preparation.StartCapturePreparation(ctx, l, progress); err != nil {
			return err
		}
		return ErrCapturePending
	case "pending":
		return ErrCapturePending
	case "ready":
		return p.Assessment.AdmitCapture(ctx, l)
	case "failed":
		return CapturePermanentFailure{Code: "CAPTURE_PREPARATION_FAILED"}
	case "cancelled":
		return CapturePermanentFailure{Code: "CAPTURE_PREPARATION_CANCELLED"}
	default:
		return ErrInvalid
	}
}
