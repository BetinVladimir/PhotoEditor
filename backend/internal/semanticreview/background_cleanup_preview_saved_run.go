package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

// SaveBackgroundCleanupPreview creates/replays the authenticated frozen draft only.
// Publication, resource reservation and task sealing must follow separately.
func SaveBackgroundCleanupPreview(ctx context.Context, db *pgxpool.Pool, who auth.Identity, p BackgroundCleanupPreviewParams) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	body, sha, err := FreezeBackgroundCleanupPreviewParams(p)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = db.QueryRow(ctx, "SELECT photo.save_semantic_background_cleanup_preview($1,$2,$3,$4,$5,$6,$7)",
		p.TenantID, p.IntentID, who.UserID, who.SessionID, p.TargetID, string(body), sha).Scan(&id)
	if ctx.Err() != nil {
		return uuid.Nil, ctx.Err()
	}
	if err != nil {
		return uuid.Nil, err
	}
	if id == uuid.Nil || id == p.WorkflowID {
		return uuid.Nil, ErrInvalid
	}
	return id, nil
}
