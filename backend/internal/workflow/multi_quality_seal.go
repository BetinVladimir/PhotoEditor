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

// SealMultiQuality performs the draft-only quality activation after full verified byte
// evidence is bound to exact current SQL locators and original occurrences.
// An already sealed graph is not reactivated; callers must observe its state.
func SealMultiQuality(ctx context.Context, db *pgxpool.Pool, plan MultiQualityPlan, content v2render.MultiResultContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateMultiQualityIntent(plan); err != nil {
		return err
	}
	if db == nil {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompileMultiQualityPlan(plan.source, content)
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
	root, image, err := content.PreviewDocuments(plan.source)
	if err != nil {
		return err
	}
	originals, err := content.PreviewOriginalInventory(plan.source)
	if err != nil {
		return err
	}
	witnesses, err := content.PreviewWitnesses(plan.source)
	if err != nil {
		return err
	}
	return withMultiPreviewDocumentTx(ctx, db, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		var accepted bool
		if err := tx.QueryRow(ctx, "SELECT photo.seal_v29_multi_quality($1,$2,$3,$4,$5::jsonb,$6::jsonb)", plan.source.Tenant, plan.run, string(root), string(image), string(originals), string(witnesses)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
}
