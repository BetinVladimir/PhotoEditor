package semanticproposal

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Presence is only a routing hint. Full manifest restoration binds the original
// anchor and every target; absence never grants authority over another path.
func (s SQLTransferSourceReader) readNativeTargetCheckpoint(ctx context.Context, who auth.Identity, request Request) ([]byte, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", false, err
	}
	if s.DB == nil || who.UserID != request.ActorID {
		return nil, "", false, v2input.ErrInvalid
	}
	var body, sha string
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT manifest_text,manifest_sha FROM photo.semantic_native_target_checkpoint($1,$2,$3,$4,$5,$6,$7)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, request.RequestSHA, request.Intent.TargetSnapshotID).Scan(&body, &sha)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", false, err
	}
	return []byte(body), sha, true, nil
}
