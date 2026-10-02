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

// Prepare performs metadata/byte proofs without creating a job. Call with
// autocommit reads; the future admission transaction must recheck authority.
func (s Service) Prepare(ctx context.Context, tenant, asset, actor, session uuid.UUID, req RegionPreviewRequest) (PreparedRegion, error) {
	var empty PreparedRegion
	raw, err := canonical.Marshal(req)
	if err != nil {
		return empty, ErrRegionInvalid
	}
	if _, err = DecodeRegionPreviewRequest(raw); err != nil {
		return empty, err
	}
	if s.Objects == nil {
		return empty, ErrRegionInvalid
	}
	source, err := ReadRegionSource(ctx, s.DB, s.Bucket, tenant, asset, req.VersionID, actor, session)
	if err != nil {
		return empty, err
	}
	recipe, err := readRegionMetadata(ctx, s.DB.QueryRow(ctx, "SELECT * FROM photo.v26_region_recipe($1,$2,$3,$4,$5,$6,$7)", tenant, asset, req.VersionID, actor, session, req.Recipe.ArtifactID, req.Recipe.SHA256), s.Bucket)
	if err != nil {
		return empty, err
	}
	expected := v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}
	if recipe.Ref != expected || recipe.ProjectID != source.ProjectID {
		return empty, ErrRegionInvalid
	}
	evidence, err := LoadRegionEvidence(ctx, s.Objects, req, recipe.Object, source.Ref, source.Object)
	if err != nil {
		return empty, err
	}
	plan, err := freezeRegionPlan(tenant, asset, source.ProjectID, s.Bucket, req, source, recipe, evidence)
	if err != nil {
		return empty, err
	}
	return PreparedRegion{ProjectID: source.ProjectID, SourceMetadata: source, RecipeMetadata: recipe, Evidence: evidence, Plan: plan}, nil
}
