package v2input

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type PublishedCaptureContext struct {
	Body     json.RawMessage
	Context  Ref
	Shots    Ref
	Consents Ref
}

// PublishCaptureContext makes dependencies immutable before publishing their
// referring context. Existing ShotList documents keep their frozen artifact ID.
func PublishCaptureContext(ctx context.Context, stager CaptureDocumentStager, documents DocumentPublisher,
	lease CaptureAssessmentLease, evidence FrozenCaptureContextEvidence) (PublishedCaptureContext, error) {
	var result PublishedCaptureContext
	if stager == nil || documents == nil || !validCaptureLease(lease) {
		return result, ErrInvalid
	}
	run, _, _, err := CaptureDraftIDs(lease.JobID)
	if err != nil {
		return result, err
	}
	shotBody, err := CompileCaptureShotList(evidence.Shots)
	if err != nil || !bytes.Equal(shotBody, evidence.ShotBody) {
		return result, ErrInvalid
	}
	shotSHA, err := canonical.Hash(evidence.Shots)
	if err != nil {
		return result, err
	}
	consentJCS, err := canonical.Bytes(evidence.ConsentBody)
	if err != nil || !bytes.Equal(consentJCS, evidence.ConsentBody) {
		return result, ErrInvalid
	}
	consentSHA, err := canonical.Hash(json.RawMessage(evidence.ConsentBody))
	if err != nil {
		return result, ErrInvalid
	}
	shots := Ref{uuid.NewSHA1(lease.JobID, []byte("capture.empty-shot")), shotSHA}
	if evidence.Selection.ShotDocument != nil {
		shots = Ref{evidence.Selection.ShotDocument.ArtifactID, evidence.Selection.ShotDocument.SHA256}
	}
	// Validate before any object writes, using a provisional consent ID. Hashes
	// are final; the publisher supplies the reserved artifact IDs afterward.
	if _, err = evidence.Compile(lease, shots, Ref{uuid.NewSHA1(lease.JobID, []byte("capture.consents")), consentSHA}); err != nil {
		return result, err
	}
	if evidence.Selection.ShotDocument == nil {
		shots, err = PublishCaptureDocument(ctx, stager, documents, lease, run, "v24_capture_shots", shotBody)
		if err != nil {
			return result, err
		}
	}
	consents, err := PublishCaptureDocument(ctx, stager, documents, lease, run, "v24_capture_consents", evidence.ConsentBody)
	if err != nil {
		return result, err
	}
	body, err := evidence.Compile(lease, shots, consents)
	if err != nil {
		return result, err
	}
	ref, err := PublishCaptureDocument(ctx, stager, documents, lease, run, "v24_capture_context", body)
	if err != nil {
		return result, err
	}
	return PublishedCaptureContext{body, ref, shots, consents}, nil
}
