package v2input

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

func PublishCapturePrepareParams(ctx context.Context, documents DocumentPublisher, l CaptureAssessmentLease,
	run uuid.UUID, config CaptureDraftConfig) (Ref, error) {
	if documents == nil || !validCaptureLease(l) || run == uuid.Nil || config.ProjectID != l.ProjectID || config.Capture.JobID != l.JobID {
		return Ref{}, ErrInvalid
	}
	body, err := canonical.Marshal(config.Capture.Params)
	if err != nil || string(body) != config.Capture.ParamsJCS {
		return Ref{}, ErrInvalid
	}
	hash, err := canonical.Hash(config.Capture.Params)
	if err != nil || hash != config.Capture.ParamsSHA {
		return Ref{}, ErrInvalid
	}
	result, err := documents.Publish(ctx, artifactjson.Scope{Tenant: l.TenantID, Run: run, Owner: l.JobID, Purpose: "v24_capture_prepare_params"}, body)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{ArtifactID: result.ID, SHA256: result.SHA256}
	if !validRef(ref) || ref.SHA256 != hash {
		return Ref{}, ErrInvalid
	}
	return ref, nil
}
