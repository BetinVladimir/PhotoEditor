package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// Materialize persists one already prepared node and its preceding-stage edge.
// Decode metadata ID must identify the canonical inspect document parsed earlier.
func (r IngestPlanRepository) Materialize(ctx context.Context, tenant, run uuid.UUID, task PreparedTask, metadata *uuid.UUID) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil || task.ID == uuid.Nil || !validExpansionRef(task.Params) || len(task.Inputs) != 1 || !validExpansionRef(task.Inputs[0]) {
		return false, errors.New("invalid ingest task")
	}
	if (task.Operation == "asset.decode") != (metadata != nil) || metadata != nil && *metadata == uuid.Nil {
		return false, errors.New("invalid ingest metadata binding")
	}
	switch task.Operation {
	case "asset.verify", "asset.inspect", "asset.decode":
	default:
		return false, errors.New("invalid ingest operation")
	}
	digest, err := repairInputDigest(task.Operation, task.Version, task.Params, task.Inputs)
	if err != nil || digest != task.InputDigest {
		return false, errors.New("invalid ingest input digest")
	}
	if _, err = ParseResourceEstimate(task.ExecutionProfile); err != nil {
		return false, err
	}
	raw, err := canonical.Marshal(task)
	if err != nil {
		return false, err
	}
	var saved bool
	err = r.DB.QueryRow(ctx, "SELECT photo.materialize_ingest_task($1,$2,$3,$4)", tenant, run, raw, metadata).Scan(&saved)
	return saved, err
}
