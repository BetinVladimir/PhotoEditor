package main

import (
	"context"
	"errors"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2sessions"
)

func analysisMemory(name string, fallback int64) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, errors.New("invalid " + name)
	}
	return value, nil
}

func analysisDefaultVRAM(backend string) int64 {
	if backend == "mps" {
		// Match the 10 GB Metal verification policy; 10 GiB exceeds it.
		return 10_000_000_000
	}
	return 10 << 30
}

// The V22 SQL slice is forward-only and may not be installed in older
// deployments. The route remains unavailable until its complete admission
// boundary is present; no API-role connection receives scheduler privileges.
func buildAnalysisAdmission(ctx context.Context, api *pgxpool.Pool, dsn string,
	objects *objectstore.Store, bucket string) (v2sessions.AnalysisAdmission, func(), error) {
	noop := func() {}
	var installed bool
	if err := api.QueryRow(ctx, `SELECT
	 to_regprocedure('photo.v22_analysis_replay(uuid,uuid,uuid,uuid,text,text)') IS NOT NULL AND
	 to_regprocedure('photo.preflight_v22_analysis(uuid,uuid,bigint,uuid,jsonb,uuid,uuid)') IS NOT NULL AND
	 to_regprocedure('photo.v22_analysis_selection_sources(uuid,uuid,bigint,uuid,jsonb,uuid,uuid)') IS NOT NULL AND
	 to_regprocedure('photo.freeze_v22_analysis_snapshot(uuid,uuid,uuid,bigint,uuid,jsonb,uuid,uuid,text)') IS NOT NULL AND
	 to_regprocedure('photo.v22_session_policy_ready(uuid,uuid,jsonb,text)') IS NOT NULL AND
	 to_regprocedure('photo.begin_v22_analysis_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,jsonb,uuid,uuid,text,text,jsonb,bigint,text,text)') IS NOT NULL AND
	 to_regprocedure('photo.save_v22_analysis_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)') IS NOT NULL AND
	 to_regprocedure('photo.read_v22_analysis_recovery(uuid,uuid,uuid,uuid,text,text)') IS NOT NULL AND
	 to_regprocedure('photo.v22_analysis_image_sources(uuid,uuid)') IS NOT NULL AND
	 to_regprocedure('photo.v22_analysis_recipe_sources(uuid,uuid)') IS NOT NULL AND
	 to_regprocedure('photo.v22_analysis_image_child(uuid,uuid,uuid,text,uuid,text,text)') IS NOT NULL AND
	 to_regprocedure('photo.v22_analysis_consent_sources(uuid,uuid)') IS NOT NULL AND
	 to_regprocedure('photo.pin_v22_analysis_consents(uuid,uuid,jsonb,text,bigint,text)') IS NOT NULL AND
	 to_regprocedure('photo.stage_v22_analysis_describes(uuid,uuid,jsonb)') IS NOT NULL AND
	 to_regprocedure('photo.reserve_v22_analysis_document(uuid,uuid,uuid,text,text,bigint,text)') IS NOT NULL AND
	 to_regprocedure('photo.complete_v22_analysis_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)') IS NOT NULL AND
	 to_regprocedure('photo.reserve_v2_input_document(uuid,uuid,uuid,text,text,bigint,text)') IS NOT NULL AND
	 to_regprocedure('photo.complete_v2_input_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)') IS NOT NULL AND
	 to_regprocedure('photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb)') IS NOT NULL AND
	 to_regprocedure('photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb)') IS NOT NULL AND
	 to_regprocedure('photo.v2_admission_ticket(uuid,uuid,text,text)') IS NOT NULL AND
	 to_regprocedure('photo.seal_v2_graph(uuid,uuid)') IS NOT NULL`).Scan(&installed); err != nil {
		return nil, noop, err
	}
	if !installed {
		return nil, noop, nil
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("analysis database configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	scheduler, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, noop, errors.New("analysis database unavailable")
	}
	if err = scheduler.Ping(ctx); err != nil {
		scheduler.Close()
		return nil, noop, errors.New("analysis database unavailable")
	}
	backend := os.Getenv("PHOTO_V22_BACKEND")
	if backend == "" {
		backend = "cuda"
	}
	ram, err := analysisMemory("PHOTO_V22_RAM_BYTES", 24<<30)
	if err != nil {
		scheduler.Close()
		return nil, noop, err
	}
	vram, err := analysisMemory("PHOTO_V22_VRAM_BYTES", analysisDefaultVRAM(backend))
	if err != nil {
		scheduler.Close()
		return nil, noop, err
	}
	coordinator, err := (v2input.SessionAnalysisRuntime{DB: scheduler,
		Objects: objects, Bucket: bucket, Backend: backend,
		RAMLimitBytes: ram, VRAMLimitBytes: vram}).Coordinator()
	if err != nil {
		scheduler.Close()
		return nil, noop, err
	}
	return coordinator, scheduler.Close, nil
}
