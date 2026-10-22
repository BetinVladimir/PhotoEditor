package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ReserveRetouchPreviewQuota freezes the compiled profile and reserves result
// storage independently of backend document tickets. It does not queue a task.
func ReserveRetouchPreviewQuota(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	preview uuid.UUID, frozen FrozenRetouchPreviewTask, source []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || preview == uuid.Nil {
		return false, ErrInvalid
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return false, err
	}
	var params RetouchPreviewParams
	if decodeStrict(frozen.frozen.closure.ParamsJCS, &params) != nil || preview == params.WorkflowID ||
		draft.ID != uuid.NewSHA1(preview, []byte("semantic.preview_retouch_region:"+params.TargetID.String())) {
		return false, ErrInvalid
	}
	if _, err := v2input.ResolveSemanticSourceMetadata(params.Source, source); err != nil {
		return false, ErrInvalid
	}
	body, sha, err := FreezeRetouchPreviewParams(params)
	if err != nil || string(body) != string(frozen.frozen.closure.ParamsJCS) || sha != frozen.frozen.params.SHA256 {
		return false, ErrInvalid
	}
	var accepted bool
	err = db.QueryRow(ctx, "SELECT photo.reserve_semantic_retouch_preview_quota($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)",
		params.TenantID, params.IntentID, who.UserID, who.SessionID, params.TargetID, preview, string(body), sha, string(source), draft.ExecutionProfile).Scan(&accepted)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return accepted, err
}
