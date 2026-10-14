package workflow

import (
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Explicit authored contract; no migrations are applied during startup.
func automaticReplacementRequiredFunctions()[]string{return []string{
 "photo.lock_v29_replacement_detector_preparation(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid)",
 "photo.save_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid)",
 "photo.read_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid)",
 "photo.reserve_v29_replacement_detector_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb)",
 "photo.complete_v29_replacement_detector_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb)",
 "photo.reserve_v29_replacement_detector_output(uuid,uuid,uuid,jsonb)",
 "photo.release_v29_replacement_detector_output_quota(uuid,uuid)",
 "photo.materialize_v29_replacement_detector_task(uuid,uuid,jsonb,jsonb)",
 "photo.reserve_v29_replacement_detector_transport(uuid,uuid,uuid,text,text,bigint,text,text,jsonb)",
 "photo.complete_v29_replacement_detector_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,text,jsonb)",
 "photo.read_v29_replacement_detector_transport_admission(uuid,uuid,uuid,jsonb)",
 "photo.create_v29_replacement_detector_workflow(uuid,uuid,text,jsonb)",
 "photo.v29_replacement_detector_input_object(uuid,uuid,uuid,text,uuid,text,jsonb)",
 "photo.register_v29_replacement_detector_inputs(uuid,uuid,jsonb,jsonb,jsonb)",
 "photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb)",
 "photo.read_v29_replacement_detector_seal_receipt(uuid,uuid,text)",
 "photo.activate_v29_replacement_detector(uuid,uuid,jsonb,jsonb)",
 "photo.read_v29_replacement_detector_admission(uuid,uuid,text,uuid)",
 "photo.ensure_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid)",
 "photo.v29_replacement_detector_adoption_evidence(uuid,uuid,integer,bigint,uuid,text,jsonb)",
 "photo.adopt_v29_replacement_detector(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb)",
 "photo.v29_replacement_detector_adoption_committed(uuid,uuid,text,uuid)",
 "photo.next_v29_replacement_detector_adoption(uuid)",
 "photo.read_v29_replacement_detector_decision(uuid,uuid,text,uuid)",
 "photo.read_v29_replacement_detector_lines(uuid,uuid,jsonb)",
 "photo.reserve_v29_replacement_segments_document(uuid,uuid,uuid,text,text,bigint,text,jsonb)",
 "photo.complete_v29_replacement_segments_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb)",
 "photo.read_v29_replacement_segments_ready(uuid,uuid,uuid,text,bigint,text,jsonb)",
 "photo.lock_v29_auto_perspective_replacement_identity(uuid,text,jsonb,text)",
 "photo.save_v29_auto_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,jsonb,text)",
 "photo.read_v29_auto_perspective_replacement_draft(uuid,text,jsonb,text)",
 "photo.reserve_v29_auto_perspective_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb)",
 "photo.complete_v29_auto_perspective_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb)",
 "photo.stage_v29_auto_perspective_replacement_task(uuid,uuid,text,jsonb)",
 "photo.reserve_v29_auto_perspective_replacement_transport_document(uuid,uuid,uuid,text,text,bigint,text,jsonb,text)",
 "photo.complete_v29_auto_perspective_replacement_transport_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb,text)",
 "photo.read_v29_auto_perspective_replacement_published_transport(uuid,uuid,uuid,text,jsonb)",
 "photo.register_v29_auto_perspective_replacement_transport(uuid,uuid,text,jsonb,jsonb,text)",
 "photo.seal_v29_auto_perspective_replacement_graph(uuid,uuid,uuid,text,jsonb)",
 "photo.read_v29_auto_perspective_replacement_queued_receipt(uuid,uuid,uuid,uuid,text,jsonb,text)",
 "photo.probe_v29_auto_perspective_publication(uuid,text,jsonb,text)",
}}
func verifyAutomaticReplacementInstallation(ctx context.Context,pool *pgxpool.Pool)error{
 if err:=ctx.Err();err!=nil{return err};if pool==nil{return errors.New("automatic replacement database unavailable")}
 bounded,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 var installed bool
 err:=pool.QueryRow(bounded,`SELECT current_user='photo_scheduler' AND EXISTS(SELECT 1 FROM pg_proc resolver WHERE resolver.oid=to_regprocedure('photo.v29_perspective_execution_plan(uuid,uuid)') AND strpos(pg_get_functiondef(resolver.oid),'read_v29_auto_perspective_replacement_queued_receipt')>0) AND coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE'),false)),false)
 FROM unnest($1::text[]) required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`,automaticReplacementRequiredFunctions()).Scan(&installed)
 if err!=nil||!installed{return errors.New("automatic replacement SQL installation unavailable")};return verifyPerspectiveReplacementExecutionInstallation(ctx,pool)
}
