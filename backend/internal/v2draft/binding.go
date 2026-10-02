package v2draft

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type BindingResolver struct {
	DB      *pgxpool.Pool
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}

// Current returns the immutable WorkingImage binding for the newest accepted
// asset version. Reading the object outside the authorization transaction is
// safe because its exact version and digest are pinned by the query.
func (s BindingResolver) Current(ctx context.Context, who auth.Identity,
	tenantID, assetID uuid.UUID) (v2input.SessionVersionBinding, error) {
	binding, _, err := s.CurrentScoped(ctx, who, tenantID, assetID)
	return binding, err
}

// CurrentScoped also returns the authorized project identity needed by the
// artifact provenance catalog for every nested recipe reference.
func (s BindingResolver) CurrentScoped(ctx context.Context, who auth.Identity,
	tenantID, assetID uuid.UUID) (v2input.SessionVersionBinding, uuid.UUID, error) {
	binding, projectID, _, err := s.CurrentScopedImage(ctx, who, tenantID, assetID)
	return binding, projectID, err
}

// CurrentScopedImage also exposes the verified source ICC lock needed by a
// FrozenRecipe, without re-reading mutable asset metadata.
func (s BindingResolver) CurrentScopedImage(ctx context.Context, who auth.Identity,
	tenantID, assetID uuid.UUID) (v2input.SessionVersionBinding, uuid.UUID, review.WorkingImage, error) {
	var binding v2input.SessionVersionBinding
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return binding, uuid.Nil, review.WorkingImage{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if assetID == uuid.Nil {
		return binding, uuid.Nil, review.WorkingImage{}, domain.NewError("NOT_FOUND", 404)
	}
	var projectID uuid.UUID
	var bucket, outputRole string
	var object objectstore.Object
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			e := tx.QueryRow(ctx, `SELECT a.project_id,v.id,m.id,m.sha256,
			 m.bytes,m.bucket,m.object_key,m.object_version,m.media_type,m.output_role
			 FROM photo.assets a JOIN LATERAL (
			  SELECT id,master_artifact_id,content_hash,review_state
			  FROM photo.asset_versions WHERE tenant_id=a.tenant_id AND asset_id=a.id
			  ORDER BY version_no DESC LIMIT 1) v ON true
			 JOIN photo.artifacts m ON m.tenant_id=a.tenant_id
			  AND m.id=v.master_artifact_id AND m.sha256=v.content_hash
			 WHERE a.tenant_id=$1 AND a.id=$2 AND a.deleted_at IS NULL
			  AND a.upload_state='verified' AND v.review_state='accepted'
			  AND m.state='ready' AND m.verified_at IS NOT NULL
			  AND m.object_version IS NOT NULL AND m.object_version NOT IN('','null')
			  AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp())`,
				tenantID, assetID).Scan(&projectID, &binding.AssetVersionID,
				&binding.Source.ArtifactID, &binding.Source.SHA256,
				&object.Bytes, &bucket, &object.Key, &object.VersionID,
				&object.ContentType, &outputRole)
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			if e != nil {
				return e
			}
			_, e = project.Authorize(ctx, tx, tenantID, projectID,
				who.UserID, role, domain.Editor, false)
			return e
		})
	if err != nil {
		return v2input.SessionVersionBinding{}, uuid.Nil, review.WorkingImage{}, err
	}
	if bucket != s.Bucket || outputRole != "working_image" ||
		object.ContentType != "application/json" || object.Bytes < 2 ||
		object.Bytes > 1048576 {
		return v2input.SessionVersionBinding{}, uuid.Nil, review.WorkingImage{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	object.SHA256 = binding.Source.SHA256
	image, err := v2input.ReadVerifiedWorkingImage(ctx, s.Objects, object, binding.Source)
	if err != nil {
		return v2input.SessionVersionBinding{}, uuid.Nil, review.WorkingImage{}, err
	}
	binding.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		return v2input.SessionVersionBinding{}, uuid.Nil, review.WorkingImage{}, err
	}
	return binding, projectID, image, nil
}
