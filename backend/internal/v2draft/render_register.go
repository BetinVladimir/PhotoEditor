package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// RegisterFrozenRenderRecipe validates exact uploaded bytes before SQL records
// source provenance. Current source/editor/session checks are repeated in SQL.
// This is not graph admission or provider/semantic authority approval.
func (p Publisher) RegisterFrozenRenderRecipe(ctx context.Context, who auth.Identity, tenantID, assetVersionID, documentID uuid.UUID, ref v2input.Ref, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(raw) < 2 || len(raw) > 1048576 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	body := append([]byte(nil), raw...)
	recipe, err := DecodeFrozenRenderRecipe(body)
	sum := sha256.Sum256(body)
	if err != nil || tenantID == uuid.Nil || documentID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || assetVersionID == uuid.Nil || recipe.Scalars.Source.AssetVersionID != assetVersionID || !isDocumentRef(ref) || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if p.DB == nil {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var registered bool
	err = tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT photo.register_v26_render_recipe($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenantID, assetVersionID, documentID, who.UserID, who.SessionID, ref.ArtifactID, ref.SHA256, recipe.Scalars.Source.Source.ArtifactID, recipe.Scalars.Source.Source.SHA256, string(body)).Scan(&registered)
	})
	if err != nil {
		return err
	}
	if !registered {
		return domain.NewError("REVISION_CONFLICT", 409)
	}
	return nil
}
