package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LoadedDonorTaskPlan joins catalog-read image evidence to the exact compiled
// plan. It is not sealed: masks, observations, confirmation, producer ancestry,
// current consent and target qualification remain assembly requirements.
type LoadedDonorTaskPlan struct {
	Plan        v2portrait.DonorTaskPlan
	Images      DonorImageEvidence
	patchWidth  int64
	patchHeight int64
	runtimeSHA  string
	capacity    v2portrait.DonorResourceEstimate
}

func LoadAndCompileDonorTaskPlan(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	params v2portrait.OpenEyesParams, objects v2input.SessionAnalysisObjectReader,
	baseCatalog, donorCatalog ArtifactCatalog, patchWidth, patchHeight int64,
	runtimeSHA string, capacity v2portrait.DonorResourceEstimate) (LoadedDonorTaskPlan, error) {
	var zero LoadedDonorTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	body, _, err := params.Compile()
	if err != nil {
		return zero, err
	}
	if _, err := domain.ParseDigest(runtimeSHA); err != nil {
		return zero, v2input.ErrInvalid
	}
	if patchWidth < 1 || patchHeight < 1 || patchWidth > 1048576 || patchHeight > 1048576/patchWidth {
		return zero, v2input.ErrInvalid
	}
	images, err := LoadDonorImageObjects(ctx, who, tenant, params, objects, baseCatalog, donorCatalog)
	if err != nil {
		return zero, err
	}
	plan, err := v2portrait.CompileDonorTaskPlan(ctx, params.Base, params.Donor, images.SourceRaw, images.DonorRaw, body, patchWidth, patchHeight, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	// The same typed manifest edges must have been authorized; never accept an
	// inventory whose hashes match while its version or parent binding differs.
	if len(plan.PhysicalImageOccurrences) != len(images.Physical) {
		return zero, v2input.ErrInvalid
	}
	for i, occurrence := range plan.PhysicalImageOccurrences {
		if occurrence != images.Physical[i].Occurrence {
			return zero, v2input.ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return LoadedDonorTaskPlan{Plan: plan, Images: images, patchWidth: patchWidth, patchHeight: patchHeight, runtimeSHA: runtimeSHA, capacity: capacity}, nil
}

func (s SQLArtifactCatalog) LoadAndCompileDonorTaskPlan(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	params v2portrait.OpenEyesParams, objects v2input.SessionAnalysisObjectReader, pw, ph int64, runtimeSHA string,
	capacity v2portrait.DonorResourceEstimate) (LoadedDonorTaskPlan, error) {
	if s.AssetVersionID != params.Base.AssetVersionID {
		return LoadedDonorTaskPlan{}, v2input.ErrInvalid
	}
	donor := s
	donor.AssetVersionID = params.Donor.AssetVersionID
	return LoadAndCompileDonorTaskPlan(ctx, who, tenant, params, objects, s, donor, pw, ph, runtimeSHA, capacity)
}
