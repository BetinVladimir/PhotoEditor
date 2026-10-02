package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

// SealInspection validates the complete persisted initial stage. It does not
// mark any task ready or equate this barrier with final ingest completion.
func (r IngestPlanRepository) SealInspection(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, errors.New("invalid ingest barrier")
	}
	var sealed bool
	err := r.DB.QueryRow(ctx, "SELECT photo.seal_ingest_inspection($1,$2)", tenant, run).Scan(&sealed)
	return sealed, err
}
