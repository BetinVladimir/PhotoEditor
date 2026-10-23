package main

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/workflow"
)

// The production loader deliberately returns the frozen config, not its envelope
// ID. Reuse its existing scheduler-authorized context for that persisted ID;
// there is no direct table SELECT, new function, or privilege expansion.
func validatePersistedPreset(ctx context.Context, db workflow.ActivationRows, j Job) error {
	rows, e := db.Query(ctx, "SELECT DISTINCT (preset->>'id')::uuid,preset->>'hash' FROM photo.download_export_context($1,$2)", j.Tenant, j.Run)
	if e != nil {
		return e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id uuid.UUID
		var hash string
		count++
		if e = rows.Scan(&id, &hash); e != nil {
			return e
		}
		if count != 1 || id != j.Preset || hash != j.PresetHash {
			return errors.New("persisted preset envelope binding drift")
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if count != 1 {
		return errors.New("missing exact persisted preset envelope")
	}
	return nil
}
func validateBatchBinding(batch workflow.DownloadExportBatch, j Job) error {
	hash, e := canonical.Hash(batch.Spec.Preset)
	if e != nil {
		return e
	}
	if batch.JobID != j.Job || batch.SnapshotID != j.Selection || batch.Spec.RunID != j.Run || hash != j.PresetHash {
		return errors.New("actual batch job/selection/run/preset drift")
	}
	return nil
}

// The only publication path is behind both exact admission and source guards.
func approvedBatchThenPublish(batch workflow.DownloadExportBatch, j Job, sources []SourceBinding, publish func() (bool, error)) (bool, error) {
	if e := validateBatchBinding(batch, j); e != nil {
		return false, e
	}
	if e := validateSourceBatch(batch, sources); e != nil {
		return false, e
	}
	return publish()
}
