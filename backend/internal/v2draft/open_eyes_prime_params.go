package v2draft

import (
	"context"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// PrimeParamsIntent prepares the first durable Params publication. Recovery
// then reloads all original evidence and current authority independently; this
// returned plan is never accepted as a shortcut for graph admission.
func (s OpenEyesRecoveryDependencies) PrimeParamsIntent(ctx context.Context,
	who auth.Identity, tenant, project, run, workflow uuid.UUID, ordinal int,
	params v2portrait.OpenEyesParams, pin v2portrait.DonorQualificationPin,
	manual DonorManualProducer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenant == uuid.Nil || project == uuid.Nil || project != s.Project ||
		run == uuid.Nil || workflow == uuid.Nil || ordinal < 0 || ordinal >= 5000 ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		s.Loader.validateConfiguration() != nil || s.Params.DB == nil ||
		s.Params.Publisher == nil || params.Base.AssetVersionID != s.Base ||
		params.Donor.AssetVersionID != s.Donor {
		return v2input.ErrInvalid
	}
	if _, err := v2input.TaskID(workflow, "portrait.open_eyes", ordinal); err != nil {
		return err
	}
	if _, err := pin.Compile(); err != nil {
		return err
	}
	located, err := v2portrait.LoadDonorQualificationEvidence(ctx,
		s.Loader.QualificationCatalog, s.Loader.Source.Objects, who,
		tenant, project, pin, s.Loader.Locks)
	if err != nil {
		return err
	}
	loaded, err := LoadAndCompileDonorTaskPlan(ctx, who, tenant, params,
		s.Loader.Source.Objects, s.Loader.Source.Catalog, s.Loader.DonorCatalog,
		s.Loader.PatchWidth, s.Loader.PatchHeight, s.Loader.Locks.RuntimeLockSHA,
		s.Loader.Capacity)
	if err != nil {
		return err
	}
	prepared, err := s.Loader.Source.PrepareQualifiedDonorInputEvidence(ctx, who,
		tenant, loaded, s.Loader.DonorCatalog, manual, located)
	if err != nil {
		return err
	}
	_, err = s.Params.Publish(ctx, who, tenant, run, workflow, ordinal, loaded, prepared)
	return err
}
