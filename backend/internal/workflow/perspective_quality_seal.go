package workflow

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
)

// SealPerspectiveQuality activates the exact quality task only after
// original-byte RGB/alpha verification and locked SQL locator rechecks.
func SealPerspectiveQuality(ctx context.Context, db *pgxpool.Pool, plan PerspectiveQualityPlan, bundle v2render.PerspectivePreviewBundle, verified v2render.VerifiedPerspectivePreviewInputs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePerspectiveQualityIntent(plan); err != nil {
		return err
	}
	if db == nil || bundle.Source() != plan.source || verified.Source() != plan.source {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		return err
	}
	if rebuilt.run != plan.run || !bytes.Equal(rebuilt.params, plan.params) || !bytes.Equal(rebuilt.profile, plan.profile) || len(rebuilt.inputs) != len(plan.inputs) {
		return objectstore.ErrIntegrity
	}
	for i, ref := range rebuilt.inputs {
		if ref != plan.inputs[i] {
			return objectstore.ErrIntegrity
		}
	}
	arrays, err := verified.AdmissionJSON()
	if err != nil {
		return err
	}
	originals := bundle.Documents()
	result := bundle.Result()
	root, image, source := originals[plan.source.Result.ArtifactID.String()], originals[result.WorkingImage.ArtifactID.String()], originals[plan.source.Base.Source.ArtifactID.String()]
	if len(root) < 2 || len(image) < 2 || len(source) < 2 {
		return objectstore.ErrInvalid
	}
	return withPerspectivePreviewDocumentTx(ctx, db, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		var accepted bool
		if err := tx.QueryRow(ctx, "SELECT photo.seal_v29_perspective_quality($1,$2,$3,$4,$5,$6::jsonb)", plan.source.Tenant, plan.run, string(root), string(image), string(source), string(arrays)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
}
