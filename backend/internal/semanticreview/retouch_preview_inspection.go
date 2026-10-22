package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PreviewInspection reports persisted state, not image quality or Apply authority.
// Failed inspection leaves the prior commit outcome unknown.
func InspectRetouchPreviewRun(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	run uuid.UUID, params RetouchPreviewParams, contextRef v2input.Ref) (PreviewInspection, error) {
	empty := PreviewInspection{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || run == uuid.Nil || contextRef.ArtifactID == uuid.Nil || !shaPattern.MatchString(contextRef.SHA256) {
		return empty, ErrInvalid
	}
	body, sha, err := FreezeRetouchPreviewParams(params)
	if err != nil {
		return empty, err
	}
	result := PreviewInspection{RunID: run, TaskID: uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+params.TargetID.String()))}
	err = db.QueryRow(ctx, "SELECT run_status,task_state,graph_sealed,task_present FROM photo.inspect_semantic_retouch_preview($1,$2,$3,$4,$5,$6,$7,$8)",
		params.TenantID, run, who.UserID, who.SessionID, string(body), sha, contextRef.ArtifactID, contextRef.SHA256).Scan(&result.RunStatus, &result.TaskState, &result.GraphSealed, &result.TaskPresent)
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
