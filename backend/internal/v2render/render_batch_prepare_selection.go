package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// PrepareSelection loads authoritative document bytes for the ordered frozen
// selection, then uses normal full-closure compilation and atomic SQL staging.
// Snapshot coverage is checked by admission. Runtime choice must come from the
// server's qualified registry; this method neither selects nor activates it.
func (f SQLRenderBatchFactory) PrepareSelection(ctx context.Context, tenant uuid.UUID, who auth.Identity, batch, project, snapshot uuid.UUID, policySHA string, selection []RenderSelectionItem, execution RenderExecutionChoice) ([]CompiledRenderBatchPart, error) {
	return f.prepareSelection(ctx, tenant, who, batch, project, snapshot, policySHA, selection, execution, false, nil)
}

// PrepareQualifiedSelection additionally requires a live selected qualification,
// exact execution/model pins and unchanged qualification after document IO.
// Provider peaks still require server calibration; graphs remain blocked.
func (f SQLRenderBatchFactory) PrepareQualifiedSelection(ctx context.Context, tenant uuid.UUID, who auth.Identity, batch, project, snapshot uuid.UUID, policySHA string, selection []RenderSelectionItem, execution RenderExecutionChoice) ([]CompiledRenderBatchPart, error) {
	return f.prepareSelection(ctx, tenant, who, batch, project, snapshot, policySHA, selection, execution, true, nil)
}

func (f SQLRenderBatchFactory) prepareSelection(ctx context.Context, tenant uuid.UUID, who auth.Identity, batch, project, snapshot uuid.UUID, policySHA string, selection []RenderSelectionItem, execution RenderExecutionChoice, requireQualification bool, calibrations RenderCalibrationCatalog) ([]CompiledRenderBatchPart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || batch == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || len(policySHA) != 64 || len(selection) < 1 || len(selection) > 5000 {
		return nil, v2input.ErrInvalid
	}
	for _, ch := range policySHA {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return nil, v2input.ErrInvalid
		}
	}
	if f.SubmissionSHA != "" {
		if _, err := domain.ParseDigest(f.SubmissionSHA); err != nil {
			return nil, v2input.ErrInvalid
		}
	}
	if f.SubmissionKey != "" {
		expected, err := v2input.RunID(tenant, who.UserID, f.SubmissionKey)
		if err != nil || expected != batch || f.SubmissionSHA == "" {
			return nil, v2input.ErrInvalid
		}
	}
	if calibrations == nil {
		if err := execution.validate(); err != nil {
			return nil, err
		}
	}
	owned := append([]RenderSelectionItem(nil), selection...)
	seen := make(map[uuid.UUID]bool, len(owned))
	for _, item := range owned {
		if err := validateRenderSelectionItem(item); err != nil {
			return nil, err
		}
		if seen[item.Binding.AssetVersionID] {
			return nil, v2input.ErrInvalid
		}
		seen[item.Binding.AssetVersionID] = true
	}
	var calibration RenderCalibration
	var calibrationSHA string
	var qualification RenderQualification
	resolver := SQLRenderQualificationResolver{API: f.API}
	if requireQualification {
		var err error
		qualification, err = resolver.Resolve(ctx, who, tenant, project)
		if err != nil {
			return nil, err
		}
		if calibrations != nil {
			calibration, err = calibrations.Resolve(ctx, qualification)
			if err != nil {
				return nil, err
			}
			calibration.Samples = append([]RenderCalibrationSample(nil), calibration.Samples...)
			if err = calibration.validate(qualification); err != nil {
				return nil, err
			}
			calibrationSHA, err = canonical.Hash(calibration)
			if err != nil {
				return nil, err
			}
			execution = RenderExecutionChoice{Backend: qualification.Backend, RuntimeSHA: qualification.RuntimeSHA, TileSize: calibration.TileSize, AlphaMode: calibration.AlphaMode}
		}
		if qualification.Backend != execution.Backend || qualification.RuntimeSHA != execution.RuntimeSHA {
			return nil, domain.NewError("RENDER_UNAVAILABLE", 503)
		}
	}
	if calibrations != nil {
		f.QualificationPin = &RenderQualificationPin{RenderQualification: qualification, CalibrationSHA: calibrationSHA}
	}
	coordinator, err := f.ForProject(ctx, project)
	if err != nil {
		return nil, err
	}
	requests := make([]RenderDraftRequest, len(owned))
	for i, item := range owned {
		loader := RenderRequestLoader{Catalog: v2draft.SQLArtifactCatalog{DB: f.API, Bucket: f.SourceBucket, ProjectID: project, AssetVersionID: item.Binding.AssetVersionID}, Objects: f.Sources}
		requests[i], err = loader.Load(ctx, tenant, who, item, execution)
		if err != nil {
			return nil, err
		}
	}
	if calibrations != nil {
		for i := range requests {
			request := &requests[i]
			evidence, err := CompileRenderEvidence(request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw)
			if err != nil {
				return nil, err
			}
			selected, err := calibration.Select(qualification, int64(evidence.Source.Canvas.Width), int64(evidence.Source.Canvas.Height), evidence.Recipe.Scalars.ICCLockSHA)
			if err != nil {
				return nil, err
			}
			request.Provider = selected.Provider
		}
	}
	if requireQualification {
		for _, request := range requests {
			recipe, err := v2draft.DecodeFrozenRenderRecipe(request.RecipeRaw)
			if err != nil {
				return nil, err
			}
			if recipe.Scalars.ModelLockSHA != qualification.ModelSHA {
				return nil, domain.NewError("RENDER_UNAVAILABLE", 503)
			}
		}
		current, err := resolver.Resolve(ctx, who, tenant, project)
		if err != nil {
			return nil, err
		}
		if current != qualification {
			return nil, domain.NewError("RENDER_UNAVAILABLE", 503)
		}
	}
	if calibrations != nil {
		current, err := calibrations.Resolve(ctx, qualification)
		if err != nil {
			return nil, err
		}
		if err = current.validate(qualification); err != nil {
			return nil, err
		}
		digest, err := canonical.Hash(current)
		if err != nil {
			return nil, err
		}
		if digest != calibrationSHA {
			return nil, renderUnavailable()
		}
	}
	return coordinator.Prepare(ctx, batch, project, snapshot, policySHA, requests)
}

// PrepareCalibratedSelection selects server execution and measured provider
// costs from current qualification plus operator registry. No client execution
// choice is accepted. All frames and registry freshness are checked before stage.
func (f SQLRenderBatchFactory) PrepareCalibratedSelection(ctx context.Context, tenant uuid.UUID, who auth.Identity, batch, project, snapshot uuid.UUID, policySHA string, selection []RenderSelectionItem, calibrations RenderCalibrationCatalog) ([]CompiledRenderBatchPart, error) {
	if calibrations == nil {
		return nil, renderUnavailable()
	}
	return f.prepareSelection(ctx, tenant, who, batch, project, snapshot, policySHA, selection, RenderExecutionChoice{}, true, calibrations)
}
