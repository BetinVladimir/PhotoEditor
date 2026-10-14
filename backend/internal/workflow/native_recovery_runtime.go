package workflow

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

// Factory dependencies are server-owned. Its processor must implement complete
// original native/manual policy authority; scheduler startup cannot invent it.
// Cleanup owns only resources created by the factory, never the shared pool/store.
type SchedulerNativeRecoveryFactory func(context.Context, *pgxpool.Pool, *objectstore.Store, string) (ExpansionJob, func(), error)

func newSchedulerNativeRecovery(ctx context.Context, factory SchedulerNativeRecoveryFactory, pool *pgxpool.Pool, objects *objectstore.Store, bucket string) (ExpansionJob, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if factory == nil {
		return nil, nil, nil
	}
	job, closeResources, err := factory(ctx, pool, objects, bucket)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && (job == nil || closeResources == nil) {
		err = errors.New("native recovery factory returned incomplete dependencies")
	}
	if err != nil {
		if closeResources != nil {
			closeResources()
		}
		return nil, nil, err
	}
	return job, closeResources, nil
}

// Enabled native recovery must fail startup before claiming any work when the
// required saved-context and fenced scheduling boundaries are absent.
func verifyNativeRecoveryInstallation(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pool == nil {
		return errors.New("native recovery scheduler database unavailable")
	}
	var installed bool
	err := pool.QueryRow(ctx, `SELECT
 to_regprocedure('photo.lock_semantic_native_targets_transfer_repair_state_v3(uuid,uuid,uuid,uuid,uuid,text,text)') IS NOT NULL
 AND has_function_privilege(current_user,'photo.lock_semantic_native_targets_transfer_repair_state_v3(uuid,uuid,uuid,uuid,uuid,text,text)','EXECUTE')
 AND to_regprocedure('photo.v27_native_initial_terminal_state(uuid,uuid,uuid,uuid,uuid)') IS NOT NULL
 AND has_function_privilege('photo_api','photo.v27_native_initial_terminal_state(uuid,uuid,uuid,uuid,uuid)','EXECUTE')
 AND to_regprocedure('photo.semantic_native_input_saved_stage_state(uuid,uuid,uuid,uuid,uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.semantic_native_input_saved_anchor_document(uuid,uuid,uuid,uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.seal_semantic_native_manual_preparation(uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.enable_v27_native_manual_parent(uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.claim_v27_native_manual_parent(uuid,integer)') IS NOT NULL
 AND to_regprocedure('photo.record_v27_native_manual_parent(uuid,uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.semantic_proposal_transfer_qualification_candidates(uuid,uuid,uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.next_v27_native_parent_recovery_project(uuid)') IS NOT NULL
 AND to_regprocedure('photo.semantic_native_input_stage_presence(uuid,uuid,uuid,uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.v27_native_manual_child_terminal(uuid,uuid,uuid,uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.v27_native_manual_selection_presence(uuid,uuid,uuid,uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.save_v27_native_manual_selection(uuid,uuid,uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.read_v27_native_manual_selection(uuid,uuid,uuid,uuid,uuid,text)') IS NOT NULL
 AND has_function_privilege(current_user,'photo.save_v27_native_manual_selection(uuid,uuid,uuid,uuid,text,text)','EXECUTE')
 AND to_regprocedure('photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.seal_and_enable_v27_native_initial_pipeline(uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.claim_v27_native_initial_pipeline(uuid,integer)') IS NOT NULL
 AND to_regprocedure('photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text)') IS NOT NULL
 AND NOT has_function_privilege(current_user,'photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text)','EXECUTE')
 AND has_function_privilege(current_user,'photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text,text)','EXECUTE')
 AND to_regprocedure('photo.next_v27_native_initial_recovery_project(uuid)') IS NOT NULL`).Scan(&installed)
	if err != nil {
		return errors.New("native recovery installation check failed")
	}
	if !installed {
		return errors.New("native recovery migrations unavailable")
	}
	return nil
}

// Source reads use photo_api, a separate pool from the scheduler. Function
// existence alone cannot establish that this role can restore original inputs.
func verifyNativeRecoverySourceInstallation(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pool == nil {
		return errors.New("native recovery source database unavailable")
	}
	required := []string{
		"photo.semantic_native_input_saved_stage(uuid,uuid,uuid,uuid,uuid,uuid,text)",
		"photo.semantic_native_input_saved_stage_state(uuid,uuid,uuid,uuid,uuid,uuid,text,text)",
		"photo.semantic_native_input_saved_anchor_document(uuid,uuid,uuid,uuid,uuid,text,text)",
		"photo.semantic_native_input_stage_presence(uuid,uuid,uuid,uuid,uuid,text,text)",
		"photo.v27_native_initial_detector_presence(uuid,uuid,uuid,uuid,uuid,text)",
		"photo.v27_native_manual_selection_presence(uuid,uuid,uuid,uuid,uuid,text)",
		"photo.read_v27_native_manual_selection(uuid,uuid,uuid,uuid,uuid,text)",
		"photo.v27_native_manual_child_terminal(uuid,uuid,uuid,uuid,uuid,text)",
		"photo.semantic_proposal_transfer_qualification_candidates(uuid,uuid,uuid,uuid,jsonb)",
	}
	var installed bool
	err := pool.QueryRow(ctx, `SELECT current_user='photo_api' AND coalesce(bool_and(coalesce(has_function_privilege(current_user,to_regprocedure(signature),'EXECUTE'),false)),false) FROM unnest($1::text[]) AS required(signature)`, required).Scan(&installed)
	if err != nil {
		return errors.New("native recovery source installation check failed")
	}
	if !installed {
		return errors.New("native recovery source readers unavailable")
	}
	return ctx.Err()
}
