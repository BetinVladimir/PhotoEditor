package v2region

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type Service struct {
	DB      RegionSourceQuery
	Objects RegionObjectReader
	Bucket  string
}
type PreparedRegion struct {
	ProjectID      uuid.UUID
	SourceMetadata RegionSourceMetadata
	RecipeMetadata RegionSourceMetadata
	Evidence       CompiledRegionEvidence
	Plan           FrozenRegionPlan
}

type RegionPreparation interface {
	Prepare(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, RegionPreviewRequest) (PreparedRegion, error)
}

// Prepare supports already scoped/autocommit metadata readers. SQL admission
// independently rechecks authority after object IO and before dispatch.
func (s Service) Prepare(ctx context.Context, tenant, asset, actor, session uuid.UUID, req RegionPreviewRequest) (PreparedRegion, error) {
	source, recipe, err := s.preparationMetadata(ctx, tenant, asset, actor, session, req)
	if err != nil {
		return PreparedRegion{}, err
	}
	return s.finishPreparation(ctx, tenant, asset, req, source, recipe)
}

func (s Service) preparationMetadata(ctx context.Context, tenant, asset, actor, session uuid.UUID, req RegionPreviewRequest) (RegionSourceMetadata, RegionSourceMetadata, error) {
	var empty RegionSourceMetadata
	if err := ctx.Err(); err != nil {
		return empty, empty, err
	}
	raw, err := canonical.Marshal(req)
	if err != nil {
		return empty, empty, ErrRegionInvalid
	}
	if _, err = DecodeRegionPreviewRequest(raw); err != nil {
		return empty, empty, err
	}
	if s.Objects == nil || s.DB == nil {
		return empty, empty, ErrRegionInvalid
	}
	source, err := ReadRegionSource(ctx, s.DB, s.Bucket, tenant, asset, req.VersionID, actor, session)
	if err != nil {
		return empty, empty, err
	}
	recipe, err := readRegionMetadata(ctx, s.DB.QueryRow(ctx, "SELECT * FROM photo.v26_region_recipe($1,$2,$3,$4,$5,$6,$7)", tenant, asset, req.VersionID, actor, session, req.Recipe.ArtifactID, req.Recipe.SHA256), s.Bucket)
	if err != nil {
		return empty, empty, err
	}
	if recipe.Ref != (v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}) || recipe.ProjectID != source.ProjectID {
		return empty, empty, ErrRegionInvalid
	}
	return source, recipe, nil
}
func (s Service) finishPreparation(ctx context.Context, tenant, asset uuid.UUID, req RegionPreviewRequest, source, recipe RegionSourceMetadata) (PreparedRegion, error) {
	if err := ctx.Err(); err != nil {
		return PreparedRegion{}, err
	}
	evidence, err := LoadRegionEvidence(ctx, s.Objects, req, recipe.Object, source.Ref, source.Object)
	if err != nil {
		return PreparedRegion{}, err
	}
	plan, err := freezeRegionPlan(tenant, asset, source.ProjectID, s.Bucket, req, source, recipe, evidence)
	if err != nil {
		return PreparedRegion{}, err
	}
	return PreparedRegion{ProjectID: source.ProjectID, SourceMetadata: source, RecipeMetadata: recipe, Evidence: evidence, Plan: plan}, nil
}
