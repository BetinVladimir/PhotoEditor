package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// SaveRetouchPreview creates/replays the authenticated frozen draft only.
// Publication, resource reservation and task sealing must follow separately.
func SaveRetouchPreview(ctx context.Context, db *pgxpool.Pool, who auth.Identity, p RetouchPreviewParams, contextRef v2input.Ref, contextBody, transferredMask []byte) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	body, sha, err := FreezeRetouchPreviewParams(p)
	if err != nil {
		return uuid.Nil, err
	}
	// Own all caller-provided bytes before validation and the SQL boundary.
	contextBody = append([]byte(nil), contextBody...)
	transferredMask = append([]byte(nil), transferredMask...)
	if _, _, err := BindRetouchPreviewContext(p, contextRef, contextBody, transferredMask); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = db.QueryRow(ctx, "SELECT photo.save_semantic_retouch_preview($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)",
		p.TenantID, p.IntentID, who.UserID, who.SessionID, p.TargetID, string(body), sha, contextRef.ArtifactID, contextRef.SHA256, string(contextBody)).Scan(&id)
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
