package semanticproposal

import (
	"context"
	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// These are explicit server-owned compiler inputs for newly created stages.
// Existing checkpoints retain their exact saved qualification/resource inputs.
type nativeFacePipelineStageConfiguration struct {
	Report v2input.Ref
	Budget nativePreparationBudget
}
type nativeFacePipelineStartConfiguration struct {
	Landmarks, Parser  nativeFacePipelineStageConfiguration
	Skin               nativePreparationBudget
	TransportDocuments v2input.DocumentPublisher
}

func (c nativeFacePipelineStartConfiguration) validate() error {
	if c.TransportDocuments == nil {
		return v2input.ErrInvalid
	}
	for _, stage := range []nativeFacePipelineStageConfiguration{c.Landmarks, c.Parser} {
		if stage.Report.ArtifactID == uuid.Nil {
			return v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(stage.Report.SHA256); err != nil {
			return v2input.ErrInvalid
		}
		if err := validateNativeFaceStartBudget(stage.Budget); err != nil {
			return err
		}
	}
	const max int64 = 9007199254740991
	for _, n := range []int64{c.Skin.RAM, c.Skin.Scratch, c.Skin.Output, c.Skin.Reserved} {
		if n < 1 || n > max {
			return v2input.ErrInvalid
		}
	}
	if c.Skin.Output > max-8388608 || c.Skin.Reserved < c.Skin.Output+8388608 {
		return v2input.ErrInvalid
	}
	return nil
}

// One durable caller attempt; never waits for inference. It queues at most one
// absent child and returns an internal waiting phase. The caller retains parent
// locking/lease ownership and retries from saved checkpoints after restart.
func (b nativePreparationBootstrap) advanceInitialFacePipeline(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, configuration nativeFacePipelineStartConfiguration) (nativeFacePipelineAdvance, error) {
	if err := b.validate(ctx); err != nil {
		return nativeFacePipelineAdvance{}, err
	}
	if err := configuration.validate(); err != nil {
		return nativeFacePipelineAdvance{}, err
	}
	starter := func(work context.Context, detector detectorDraftPlan, parser nativeFaceDraftPlan, operation string) error {
		run, _, _, err := nativeInputSavedStageIdentity(detector.request.RunID, operation)
		if err != nil {
			return err
		}
		scoped := b
		scoped.Transport = v2input.ScopedPublisher{Documents: configuration.TransportDocuments, Tenant: detector.request.TenantID, Run: run}
		switch operation {
		case "face.landmarks":
			_, err = scoped.ensureFace(work, who, detector, anchor.Color, operation, configuration.Landmarks.Report, configuration.Landmarks.Budget)
		case "face.parse":
			_, err = scoped.ensureFace(work, who, detector, anchor.Color, operation, configuration.Parser.Report, configuration.Parser.Budget)
		case "mask.face_skin":
			_, err = scoped.ensureSkin(work, who, parser, anchor.Color, configuration.Skin)
		default:
			return v2input.ErrInvalid
		}
		return err
	}
	return b.Reader.advanceNativeFacePipeline(ctx, b.Scheduler, b.Catalog, who, anchor, starter)
}
