package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
)

// DefectReviewContext is supplied by the compiler's bound node context, never
// directly by an HTTP request. SQL attests canonical task output provenance;
// JSON content binding remains mandatory before accepting a user decision.
type DefectReviewContext struct{ TenantID, RunID, TaskID, ImageID, ResolvedID uuid.UUID }
type DefectReviewCreator struct{ DB artifactjson.RowQuerier }

func (c DefectReviewCreator) Create(ctx context.Context, in DefectReviewContext) (uuid.UUID, error) {
	if c.DB == nil || in.TenantID == uuid.Nil || in.RunID == uuid.Nil || in.TaskID == uuid.Nil || in.ImageID == uuid.Nil || in.ResolvedID == uuid.Nil {
		return uuid.Nil, errors.New("invalid defect review context")
	}
	var id *uuid.UUID
	if err := c.DB.QueryRow(ctx, "SELECT photo.create_defect_review($1,$2,$3,$4,$5)", in.TenantID, in.RunID, in.TaskID, in.ImageID, in.ResolvedID).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if id == nil || *id == uuid.Nil {
		return uuid.Nil, errors.New("defect review context unavailable or conflicting")
	}
	return *id, nil
}
