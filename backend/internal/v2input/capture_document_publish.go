package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

type CaptureDocumentStager interface {
	StageDocumentBody(context.Context, CaptureAssessmentLease, string, []byte) error
}

func (s SQLCaptureAssessmentStore) StageDocumentBody(ctx context.Context, lease CaptureAssessmentLease, purpose string, body []byte) error {
	if s.DB == nil || !validCaptureLease(lease) || !captureAssessmentPurpose(purpose) {
		return ErrInvalid
	}
	var staged bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.stage_capture_document_body($1,$2,$3,$4,$5)`, lease.TenantID, lease.JobID, lease.Token, purpose, string(body)).Scan(&staged); err != nil {
		return err
	}
	if !staged {
		return ErrExpired
	}
	return nil
}
func captureAssessmentPurpose(purpose string) bool {
	switch purpose {
	case "v24_capture_shots", "v24_capture_consents", "v24_capture_context", "v24_capture_assess_params":
		return true
	default:
		return false
	}
}

func PublishCaptureDocument(ctx context.Context, stager CaptureDocumentStager, documents DocumentPublisher,
	lease CaptureAssessmentLease, run uuid.UUID, purpose string, body []byte) (Ref, error) {
	if stager == nil || documents == nil || !validCaptureLease(lease) || !captureAssessmentPurpose(purpose) {
		return Ref{}, ErrInvalid
	}
	expected, _, _, err := CaptureDraftIDs(lease.JobID)
	if err != nil || expected != run {
		return Ref{}, ErrInvalid
	}
	canonicalBody, err := canonical.Bytes(body)
	limit := 1048576
	if purpose == "v24_capture_context" || purpose == "v24_capture_consents" {
		limit = 8388608
	}
	if err != nil || len(canonicalBody) > limit || string(canonicalBody) != string(body) {
		return Ref{}, ErrInvalid
	}
	hash, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		return Ref{}, err
	}
	if err = stager.StageDocumentBody(ctx, lease, purpose, body); err != nil {
		return Ref{}, err
	}
	result, err := documents.Publish(ctx, artifactjson.Scope{Tenant: lease.TenantID, Run: run, Owner: lease.JobID, Purpose: purpose}, body)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{result.ID, result.SHA256}
	if !validRef(ref) || ref.SHA256 != hash {
		return Ref{}, ErrInvalid
	}
	return ref, nil
}
