package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

// PreviewInspection reports persisted state, not image quality or Apply authority.
// Failed inspection leaves the prior commit outcome unknown.
func InspectBackgroundCleanupPreviewRun(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	run uuid.UUID, params BackgroundCleanupPreviewParams) (PreviewInspection, error) {
	empty := PreviewInspection{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || run == uuid.Nil {
		return empty, ErrInvalid
	}
	body, sha, err := FreezeBackgroundCleanupPreviewParams(params)
	if err != nil {
		return empty, err
	}
	result := PreviewInspection{RunID: run, TaskID: uuid.NewSHA1(run, []byte("semantic.preview_background_cleanup:"+params.TargetID.String()))}
	err = db.QueryRow(ctx, "SELECT run_status,task_state,graph_sealed,task_present FROM photo.inspect_semantic_background_cleanup_preview($1,$2,$3,$4,$5,$6)",
		params.TenantID, run, who.UserID, who.SessionID, string(body), sha).Scan(&result.RunStatus, &result.TaskState, &result.GraphSealed, &result.TaskPresent)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if result.GraphSealed && !result.TaskPresent || result.TaskPresent != (result.TaskState != "") {
		return empty, ErrInvalid
	}
	return result, nil
}
