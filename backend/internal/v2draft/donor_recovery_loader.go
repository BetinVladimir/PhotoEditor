package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
)

// DonorRecoveryLoader uses separately scoped base/donor catalogs and trusted
// deployment configuration. Manual producer documents must be loaded through
// their authorized provenance route; their hashes/bindings are rechecked here.
// No SQL transaction spans any object read.
type DonorRecoveryLoader struct {
	Source                  ClosureResolver
	DonorCatalog            ArtifactCatalog
	QualificationCatalog    v2portrait.DonorQualificationCatalog
	Locks                   v2qualification.DeploymentLocks
	PatchWidth, PatchHeight int64
	Capacity                v2portrait.DonorResourceEstimate
}

// validateConfiguration is shared by reload and orchestration so incomplete
// deployment dependencies are rejected before a recovery-intent SQL read.
func (s DonorRecoveryLoader) validateConfiguration() error {
	if s.Source.Catalog == nil || s.Source.Objects == nil || s.DonorCatalog == nil || s.QualificationCatalog == nil || s.PatchWidth < 1 || s.PatchHeight < 1 || s.PatchWidth > 1048576 || s.PatchHeight > 1048576/s.PatchWidth {
		return v2input.ErrInvalid
	}
	return nil
}

// Reload reconstructs original qualification/image/mask/face evidence and
// validates the rebuilt inventory. It neither publishes Params nor activates a
// task. Publish and final transactional admission must repeat current authority.
func (s DonorRecoveryLoader) Reload(ctx context.Context, who auth.Identity, tenant, project, workflow uuid.UUID,
	seed DonorParamsRecoverySeed, manual DonorManualProducer) (LoadedDonorTaskPlan, PreparedDonorInputEvidence, error) {
	var noPlan LoadedDonorTaskPlan
	var noEvidence PreparedDonorInputEvidence
	fail := func(err error) (LoadedDonorTaskPlan, PreparedDonorInputEvidence, error) {
		return noPlan, noEvidence, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if tenant == uuid.Nil || project == uuid.Nil || s.validateConfiguration() != nil {
		return fail(v2input.ErrInvalid)
	}
	raw, err := canonical.Marshal(seed.intent)
	if err != nil {
		return fail(err)
	}
	owned, err := ParseDonorParamsRecoveryIntent(ctx, who, workflow, raw)
	if err != nil {
		return fail(err)
	}
	manual.RootRaw = appendOptionalBytes(manual.RootRaw)
	manual.ParamsRaw = appendOptionalBytes(manual.ParamsRaw)
	manual.VectorsRaw = appendOptionalBytes(manual.VectorsRaw)
	qualification, err := owned.ReloadQualification(ctx, who, workflow, tenant, project, s.Locks, s.QualificationCatalog, s.Source.Objects)
	if err != nil {
		return fail(err)
	}
	loaded, err := LoadAndCompileDonorTaskPlan(ctx, who, tenant, owned.Params(), s.Source.Objects, s.Source.Catalog, s.DonorCatalog, s.PatchWidth, s.PatchHeight, owned.QualificationPin().RuntimeLockSHA, s.Capacity)
	if err != nil {
		return fail(err)
	}
	if string(loaded.Plan.Params) != owned.intent.Params || loaded.Plan.ParamsSHA != owned.intent.ParamsSHA || loaded.Plan.ReservedBytes != owned.intent.ReservedBytes {
		return fail(v2input.ErrInvalid)
	}
	prepared, err := s.Source.PrepareQualifiedDonorInputEvidence(ctx, who, tenant, loaded, s.DonorCatalog, manual, qualification)
	if err != nil {
		return fail(err)
	}
	pin, err := verifyPreparedDonorQualification(loaded, prepared)
	if err != nil {
		return fail(err)
	}
	locations, err := prepared.qualification.Objects()
	if err != nil {
		return fail(err)
	}
	if pin != owned.QualificationPin() || !reflect.DeepEqual(locations, owned.QualificationObjects()) {
		return fail(v2input.ErrInvalid)
	}
	if _, err = BindDonorParamsInventory(ctx, loaded, prepared, v2input.Ref{ArtifactID: uuid.New(), SHA256: loaded.Plan.ParamsSHA}); err != nil {
		return fail(err)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	return loaded, prepared, nil
}
