package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// AuthoredSegmentCalibrationCatalog is a distinct operator-owned segment version-1
// installation. An compose/1 or compose/2 calibration registry cannot accidentally
// satisfy this interface; evidence must cover the authored continuation build.
type AuthoredSegmentCalibrationCatalog interface {
	ResolveAuthoredSegment(context.Context, RenderQualification) (RenderCalibration, error)
}

// CalibratedAuthoredSegmentRequest is preparation evidence, never activation authority.
// Its qualification is for the authored segment CPU renderer only; upstream stage
// installations still require their own operation-specific current admission.
type CalibratedAuthoredSegmentRequest struct {
	Request          RenderDraftRequest
	Qualification    RenderQualification
	CalibrationSHA   string
	OperationVersion string
}

// LoadCalibratedAuthoredSegmentRequest uses the same concrete SQL resolver, scoped
// document reader and calibrated provider selection as ordinary render admission.
// It deliberately does not stage the ordinary compose/1 graph: the segment dispatcher must
// materialize actual predecessor outputs before its final task can be sealed.
func LoadCalibratedAuthoredSegmentRequest(ctx context.Context, api *pgxpool.Pool, objects v2input.SessionAnalysisObjectReader, bucket string, calibrations AuthoredSegmentCalibrationCatalog, tenant, project uuid.UUID, who auth.Identity, item RenderSelectionItem) (CalibratedAuthoredSegmentRequest, error) {
	if api == nil || objects == nil || bucket == "" || calibrations == nil {
		return CalibratedAuthoredSegmentRequest{}, v2input.ErrInvalid
	}
	resolver := SQLAuthoredSegmentQualificationResolver{API: api}
	qualification, err := resolver.Resolve(ctx, who, tenant, project)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	calibration, err := calibrations.ResolveAuthoredSegment(ctx, qualification)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	if err = calibration.validate(qualification); err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	calibration.Samples = append([]RenderCalibrationSample(nil), calibration.Samples...)
	digest, err := canonical.Hash(calibration)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	execution := RenderExecutionChoice{Backend: qualification.Backend, RuntimeSHA: qualification.RuntimeSHA, TileSize: calibration.TileSize, AlphaMode: calibration.AlphaMode}
	loader := RenderRequestLoader{Catalog: v2draft.SQLArtifactCatalog{DB: api, Bucket: bucket, ProjectID: project, AssetVersionID: item.Binding.AssetVersionID}, Objects: objects}
	request, err := loader.Load(ctx, tenant, who, item, execution)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	evidence, err := CompileRenderEvidence(request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	selected, err := calibration.Select(qualification, int64(evidence.Source.Canvas.Width), int64(evidence.Source.Canvas.Height), evidence.Recipe.Scalars.ICCLockSHA)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	request.Provider = selected.Provider
	if evidence.Recipe.Scalars.ModelLockSHA != qualification.ModelSHA {
		return CalibratedAuthoredSegmentRequest{}, renderUnavailable()
	}
	current, err := resolver.Resolve(ctx, who, tenant, project)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	if current != qualification {
		return CalibratedAuthoredSegmentRequest{}, renderUnavailable()
	}
	currentCalibration, err := calibrations.ResolveAuthoredSegment(ctx, qualification)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	if err = currentCalibration.validate(qualification); err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	currentSHA, err := canonical.Hash(currentCalibration)
	if err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	if currentSHA != digest {
		return CalibratedAuthoredSegmentRequest{}, renderUnavailable()
	}
	if err = ctx.Err(); err != nil {
		return CalibratedAuthoredSegmentRequest{}, err
	}
	return CalibratedAuthoredSegmentRequest{Request: request, Qualification: qualification, CalibrationSHA: digest, OperationVersion: "1"}, nil
}
