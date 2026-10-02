package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type CaptureAssessmentDraftStore interface {
	CaptureDocumentStager
	LoadDraft(context.Context, CaptureAssessmentLease) (CaptureDraftConfig, error)
	StageAssessment(context.Context, CaptureAssessmentLease, Ref, string, json.RawMessage) error
}
type CaptureContextEvidenceSource interface {
	Build(context.Context, CaptureAssessmentLease) (FrozenCaptureContextEvidence, error)
}
type CapturePreparedImageSource interface {
	Load(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (CapturePreparedDocument, error)
	ResolveProxy(context.Context, uuid.UUID, uuid.UUID, CapturePreparedDocument, CaptureSourceImage) (CaptureImageInputs, error)
}
type CaptureAssessmentPreparer struct {
	Store     CaptureAssessmentDraftStore
	Source    CapturePreparationSource
	Prepared  CapturePreparedImageSource
	Context   CaptureContextEvidenceSource
	Documents DocumentPublisher
}

// PrepareCapture resumes the single capture workflow using its frozen model
// lock and resource profile. It commits a blocked assessment shell only after
// all exact documents and the complete input inventory have been compiled.
func (p CaptureAssessmentPreparer) PrepareCapture(ctx context.Context, lease CaptureAssessmentLease) (FrozenCaptureGraph, error) {
	var graph FrozenCaptureGraph
	if p.Store == nil || p.Source == nil || p.Prepared == nil || p.Context == nil || p.Documents == nil || !validCaptureLease(lease) {
		return graph, ErrInvalid
	}
	run, workflow, _, err := CaptureDraftIDs(lease.JobID)
	if err != nil || lease.PreparationWorkflowID == nil || *lease.PreparationWorkflowID != workflow {
		return graph, ErrInvalid
	}
	config, err := p.Store.LoadDraft(ctx, lease)
	if err != nil {
		return graph, err
	}
	source, inputs, err := p.Source.LoadInputs(ctx, lease)
	if err != nil {
		return graph, err
	}
	if source.Binding != config.Capture.Params.Binding {
		return graph, ErrInvalid
	}
	prepared, err := p.Prepared.Load(ctx, lease.TenantID, run, workflow)
	if err != nil {
		return graph, err
	}
	proxy, err := p.Prepared.ResolveProxy(ctx, lease.TenantID, run, prepared, source)
	if err != nil {
		return graph, err
	}
	evidence, err := p.Context.Build(ctx, lease)
	if err != nil {
		return graph, err
	}
	published, err := PublishCaptureContext(ctx, p.Store, p.Documents, lease, evidence)
	if err != nil {
		return graph, err
	}
	var policy CapturePolicy
	if json.Unmarshal(config.Capture.Policy, &policy) != nil {
		return graph, ErrInvalid
	}
	params := CaptureAssessParams{1, source.Binding, prepared.Proxy, prepared.Faces, policy, config.Capture.Params.ModelLockSHA, published.Context}
	body, err := canonical.Marshal(params)
	if err != nil {
		return graph, err
	}
	ref, err := PublishCaptureDocument(ctx, p.Store, p.Documents, lease, run, "v24_capture_assess_params", body)
	if err != nil {
		return graph, err
	}
	task, err := TaskID(workflow, "capture.assess", 0)
	if err != nil {
		return graph, err
	}
	closure, err := CompileCaptureClosure(task, params, ref, inputs, proxy, published.Shots, published.Consents)
	if err != nil {
		return graph, err
	}
	if err = p.Store.StageAssessment(ctx, lease, ref, closure.InputDigest, config.Capture.Profile); err != nil {
		return graph, err
	}
	return FrozenCaptureGraph{run, workflow, params, ref, published.Body, inputs, proxy, published.Shots, published.Consents}, nil
}

func (s SQLCaptureAssessmentStore) StageAssessment(ctx context.Context, lease CaptureAssessmentLease, params Ref, digest string, profile json.RawMessage) error {
	if s.DB == nil || !validCaptureLease(lease) || !validRef(params) || !digestPattern.MatchString(digest) || !json.Valid(profile) {
		return ErrInvalid
	}
	_, workflow, _, err := CaptureDraftIDs(lease.JobID)
	if err != nil {
		return err
	}
	task, err := TaskID(workflow, "capture.assess", 0)
	if err != nil {
		return err
	}
	var staged bool
	if err = s.DB.QueryRow(ctx, `SELECT photo.stage_capture_assessment($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, lease.TenantID, lease.JobID, lease.Token, task, params.ArtifactID, params.SHA256, digest, profile).Scan(&staged); err != nil {
		return err
	}
	if !staged {
		return ErrExpired
	}
	return nil
}
