package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
)

type CapturePreparationStore interface {
	FreezeSnapshot(context.Context, CaptureAssessmentLease, SessionVersionBinding) (uuid.UUID, error)
	BeginDraft(context.Context, CaptureAssessmentLease, SessionVersionBinding, string, json.RawMessage, int64) (CaptureDraftConfig, error)
	LoadDraft(context.Context, CaptureAssessmentLease) (CaptureDraftConfig, error)
	StagePreparation(context.Context, CaptureAssessmentLease, Ref, string, json.RawMessage) error
}
type CapturePreparationSource interface {
	LoadInputs(context.Context, CaptureAssessmentLease) (CaptureSourceImage, CaptureImageInputs, error)
}
type CapturePreparationCommitter interface {
	CommitCapturePreparation(context.Context, CaptureAssessmentLease, uuid.UUID, uuid.UUID, GraphTask, json.RawMessage) error
}
type CapturePreparationService struct {
	Store                                            CapturePreparationStore
	Source                                           CapturePreparationSource
	Documents                                        DocumentPublisher
	Committer                                        CapturePreparationCommitter
	TransportForRun                                  func(CaptureAssessmentLease, uuid.UUID) Publisher
	Backend, ModelLockSHA                            string
	RAMLimit, VRAMLimit, ScratchLimit, ReservedBytes int64
}

func (s CapturePreparationService) StartCapturePreparation(ctx context.Context, l CaptureAssessmentLease, progress *CapturePreparationProgress) error {
	if s.Store == nil || s.Source == nil || s.Documents == nil || s.Committer == nil || s.TransportForRun == nil || !validCaptureLease(l) {
		return ErrInvalid
	}
	run, workflow, _, err := CaptureDraftIDs(l.JobID)
	if err != nil {
		return err
	}
	if progress != nil && (progress.Phase != "draft" || progress.RunID != run || progress.WorkflowID != workflow) {
		return ErrInvalid
	}
	source, inputs, err := s.Source.LoadInputs(ctx, l)
	if err != nil {
		return err
	}
	// Accepted capture receipts point at decode masters. Do not silently omit
	// a geometry artifact if a different producer appears in that binding.
	var chain struct {
		Steps []struct {
			Kind string `json:"kind"`
		} `json:"steps"`
	}
	if json.Unmarshal(source.Image.TransformChain, &chain) != nil {
		return ErrInvalid
	}
	for _, step := range chain.Steps {
		if step.Kind == "dense_warp" {
			return ErrInvalid
		}
	}
	var config CaptureDraftConfig
	if progress == nil {
		if _, err = s.Store.FreezeSnapshot(ctx, l, source.Binding); err != nil {
			return err
		}
		profile, profileErr := CompileCapturePrepareProfile(s.Backend, source.Image.Canvas.Width, source.Image.Canvas.Height,
			source.Image.Alpha != nil, s.RAMLimit, s.VRAMLimit, s.ScratchLimit)
		if profileErr != nil {
			return profileErr
		}
		config, err = s.Store.BeginDraft(ctx, l, source.Binding, s.ModelLockSHA, profile, s.ReservedBytes)
	} else {
		config, err = s.Store.LoadDraft(ctx, l)
	}
	if err != nil {
		return err
	}
	if config.Capture.Params.Binding != source.Binding {
		return ErrInvalid
	}
	params, err := PublishCapturePrepareParams(ctx, s.Documents, l, run, config)
	if err != nil {
		return err
	}
	closure, err := CompileCapturePrepareClosure(workflow, config.Capture.Params, params, inputs, nil)
	if err != nil {
		return err
	}
	if err = s.Store.StagePreparation(ctx, l, params, closure.InputDigest, config.Capture.Profile); err != nil {
		return err
	}
	transport := s.TransportForRun(l, run)
	if transport == nil {
		return ErrInvalid
	}
	return PublishCapturePreparation(ctx, s.Committer, transport, l, run, workflow,
		config.Capture.Params, params, inputs, nil)
}
