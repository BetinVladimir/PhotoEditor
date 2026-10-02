package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/domain"
)

// BoundTaskActivation is produced only after compiler input binding. A false
// result means stale or not yet eligible; it is never permission to dispatch.
type BoundTaskActivation struct {
	TaskID, ParamsID uuid.UUID
	InputDigest      string
}
type AdvancementRepository struct{ DB artifactjson.RowQuerier }

func (r AdvancementRepository) Activate(ctx context.Context, a BoundTaskActivation) (bool, error) {
	if r.DB == nil || a.TaskID == uuid.Nil || a.ParamsID == uuid.Nil {
		return false, errors.New("invalid bound task activation")
	}
	if _, err := domain.ParseDigest(a.InputDigest); err != nil {
		return false, err
	}
	var activated bool
	err := r.DB.QueryRow(ctx, "SELECT photo.activate_bound_task($1,$2,$3)", a.TaskID, a.InputDigest, a.ParamsID).Scan(&activated)
	return activated, err
}
