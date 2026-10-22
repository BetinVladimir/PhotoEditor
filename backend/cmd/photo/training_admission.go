package main

import (
 "photoeditor/backend/internal/database"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2learning"
)

// buildTrainingAdmission enables the V23 endpoint only when the immutable
// training admission, source and recovery functions are installed together.
// The API pool remains photo_api; privileged draft and source reads use a
// separate photo_scheduler connection as required by their grants.
func buildTrainingAdmission(ctx context.Context, api *pgxpool.Pool,
	dsn string, objects *objectstore.Store, bucket string) (
	v2learning.TrainingAdmission, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil { return nil, noop, err }
	// Advanced personal learning is an explicit installation. Never fall back
	// to the API credential (or libpq defaults) for scheduler authority.
	if dsn == "" { return nil, noop, nil }
	if api == nil || objects == nil || bucket == "" {
		return nil, noop, errors.New("training dependencies unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ctx = bounded
	var installed bool
	err := api.QueryRow(ctx, `SELECT
	 to_regproc('photo.v23_training_replay') IS NOT NULL AND
	 to_regproc('photo.begin_v23_training_draft') IS NOT NULL AND
	 to_regproc('photo.save_v23_training_recovery') IS NOT NULL AND
	 to_regproc('photo.save_v23_training_recovery_core') IS NOT NULL AND
	 to_regproc('photo.v23_training_inventory_profile') IS NOT NULL AND
	 to_regproc('photo.v23_training_candidate_rows') IS NOT NULL AND
	 to_regproc('photo.v23_training_model_lock') IS NOT NULL AND
	 to_regproc('photo.v23_training_calibration_source') IS NOT NULL AND
	 to_regproc('photo.v23_training_active_baseline') IS NOT NULL AND
	 to_regproc('photo.v23_training_document_source') IS NOT NULL AND
	 to_regproc('photo.v23_training_mask_roots') IS NOT NULL AND
	 to_regproc('photo.v23_training_mask_provenance') IS NOT NULL AND
	 to_regproc('photo.v23_training_frozen_restore') IS NOT NULL AND
	 to_regproc('photo.freeze_v23_training_snapshot') IS NOT NULL AND
	 to_regproc('photo.stage_v23_learning_fit') IS NOT NULL AND
	 to_regproc('photo.reserve_v23_training_frozen') IS NOT NULL AND
	 to_regproc('photo.complete_v23_training_frozen') IS NOT NULL AND
	 to_regproc('photo.reserve_v23_training_zero') IS NOT NULL AND
	 to_regproc('photo.complete_v23_training_zero') IS NOT NULL AND
	 to_regproc('photo.reserve_v23_training_document') IS NOT NULL AND
	 to_regproc('photo.complete_v23_training_document') IS NOT NULL AND
	 to_regproc('photo.seal_v2_graph') IS NOT NULL AND
	 to_regproc('photo.v2_admission_ticket') IS NOT NULL`).Scan(&installed)
	if err != nil {
		return nil, noop, err
	}
	if !installed {
		return nil, noop, errors.New("training SQL installation unavailable")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("training database configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(config)
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	scheduler, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, noop, errors.New("training database unavailable")
	}
	if err = scheduler.Ping(ctx); err != nil {
		scheduler.Close()
		return nil, noop, errors.New("training database unavailable")
	}
	source, err := v2learning.NewSQLTrainingSourceResolver(api, scheduler,
		objects, bucket)
	if err != nil {
		scheduler.Close()
		return nil, noop, err
	}
	return v2learning.SQLTrainingAdmission{DB: scheduler, Source: source,
		Runtime: v2learning.TrainingFitRuntime{DB: scheduler,
			Objects: objects, Bucket: bucket}}, scheduler.Close, nil
}
