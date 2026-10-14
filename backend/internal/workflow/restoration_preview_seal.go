package workflow

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SealRestorationPreview activates all four media tasks in one SQL transaction
// after the physical witness is rechecked against live object versions.
func SealRestorationPreview(ctx context.Context, db *pgxpool.Pool,
	plan RestorationPreviewPlan, baseRaw, candidateRaw []byte, bucket string,
	verified VerifiedRestorationPreviewInputs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRestorationPreviewIntent(plan); err != nil {
		return err
	}
	if db == nil || len(bucket) < 3 || len(bucket) > 63 ||
		verified.Source() != plan.source {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompileRestorationPreviewPlan(plan.source, plan.srgb,
		baseRaw, candidateRaw)
	if err != nil || rebuilt.run != plan.run {
		return v2input.ErrInvalid
	}
	for i := range rebuilt.docs {
		if rebuilt.docs[i].ID != plan.docs[i].ID ||
			rebuilt.docs[i].ParamsSHA != plan.docs[i].ParamsSHA {
			return v2input.ErrInvalid
		}
	}
	arrays, err := verified.AdmissionJSON()
	if err != nil {
		return err
	}
	return withPerspectivePreviewDocumentTx(ctx, db, plan.source.Tenant,
		func(ctx context.Context, tx pgx.Tx) error {
			var accepted bool
			if err := tx.QueryRow(ctx,
				"SELECT photo.seal_v28_restoration_preview($1,$2,$3,$4,$5,$6::jsonb)",
				plan.source.Tenant, plan.run, string(baseRaw), string(candidateRaw),
				bucket, string(arrays)).Scan(&accepted); err != nil {
				return err
			}
			if !accepted {
				return artifactjson.ErrStale
			}
			return nil
		})
}
