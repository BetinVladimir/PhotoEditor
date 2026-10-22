package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ShownAppliedImage is a retained result pointer, not a download grant or a new asset version.
type ShownAppliedImage struct {
	ChildRunID   uuid.UUID   `json:"child_run_id"`
	TaskID       uuid.UUID   `json:"task_id"`
	ResultID     uuid.UUID   `json:"result_id"`
	WorkingImage v2input.Ref `json:"working_image"`
}

func LoadShownAppliedImage(ctx context.Context, db artifactjson.RowQuerier, who auth.Identity, tenant, target uuid.UUID) (ShownAppliedImage, error) {
	var out ShownAppliedImage
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if db == nil || target == uuid.Nil || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return out, ErrInvalid
	}
	err := db.QueryRow(ctx, "SELECT child_run_id,task_id,result_id,artifact_id,sha256 FROM photo.semantic_shown_applied_image($1,$2,$3,$4)", tenant, target, who.UserID, who.SessionID).Scan(&out.ChildRunID, &out.TaskID, &out.ResultID, &out.WorkingImage.ArtifactID, &out.WorkingImage.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShownAppliedImage{}, ErrConflict
	}
	if err != nil {
		return ShownAppliedImage{}, err
	}
	if out.ChildRunID == uuid.Nil || (out.TaskID != uuid.NewSHA1(out.ChildRunID, []byte("semantic.adopt_shown_exposure:"+target.String())) && out.TaskID != uuid.NewSHA1(out.ChildRunID, []byte("semantic.adopt_shown_removal:"+target.String())) && out.TaskID != uuid.NewSHA1(out.ChildRunID, []byte("semantic.adopt_shown_background_cleanup:"+target.String())) && out.TaskID != uuid.NewSHA1(out.ChildRunID, []byte("semantic.adopt_shown_retouch_region:"+target.String()))) || out.ResultID == uuid.Nil || out.WorkingImage.ArtifactID == uuid.Nil || !shaPattern.MatchString(out.WorkingImage.SHA256) {
		return ShownAppliedImage{}, ErrInvalid
	}
	return out, nil
}
