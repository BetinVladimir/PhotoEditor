package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

// CheckCurrentPreviewAuthority is a read fence, never an Apply admission.
// Providers must repeat it after object IO; adoption needs transactional locks.
func CheckCurrentPreviewAuthority(ctx context.Context, db *pgxpool.Pool, who auth.Identity, tenant, intent, target uuid.UUID, request PreviewRequest) error {
	if request.Validate() != nil || tenant == uuid.Nil || intent == uuid.Nil || target == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return ErrUnavailable
	}
	var current bool
	err := db.QueryRow(ctx, `SELECT photo.semantic_preapply_preview_current($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenant, intent, target, who.UserID, who.SessionID, request.ExpectedRevision, request.TargetRevision, request.PlanSHA, request.CandidateSHA).Scan(&current)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !current {
		return ErrConflict
	}
	return nil
}
