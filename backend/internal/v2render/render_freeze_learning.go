package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Called after registration inside the same transaction: scope functions need
// the registered root. A denied model/descriptor aborts that transaction too.
// Pinned execution bytes never grant continued permission to use learning data.
func validateFrozenLearningScope(ctx context.Context, tx pgx.Tx, tid, asset uuid.UUID, who auth.Identity, root v2input.Ref, frozen v2draft.FrozenRenderRecipe) error {
	learning := frozen.Learning
	if learning == nil {
		return nil
	}
	var project uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT project_id FROM photo.assets WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tid, asset).Scan(&project); err != nil {
		return err
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT photo.v26_render_model_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) AND photo.v26_render_descriptor_scope($1,$2,$3,$4,$5,$11,$12,$13,$14,$15,$9,$10)`, tid, project, frozen.Scalars.Source.AssetVersionID, root.ArtifactID, root.SHA256, learning.Model.ArtifactID, learning.Model.SHA256, learning.VersionID, who.UserID, who.SessionID, learning.Descriptor.ArtifactID, learning.Descriptor.SHA256, learning.AnalysisID, learning.Ordinal, frozen.Scalars.ModelLockSHA).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return domain.NewError("SNAPSHOT_EXPIRED", 410)
	}
	return nil
}
