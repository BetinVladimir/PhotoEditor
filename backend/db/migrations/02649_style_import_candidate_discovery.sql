-- +goose Up
-- Restore import discovery lost when 00172 replaced the earlier candidate query.
-- Both analysis branches and the existing materialization lease remain unchanged.
-- +goose StatementBegin
DO $migration$
DECLARE before record; after record; definition text; expected_source text;
 needle text:=$anchor$ ) q LEFT JOIN photo.style_candidates existing$anchor$;
 replacement text:=$branch$  UNION ALL
  SELECT r.tenant_id,r.id,(r.config_snapshot->>'style_id')::uuid,config.id,report.id
  FROM photo.runs r
  JOIN photo.style_import_builds imported ON imported.tenant_id=r.tenant_id AND imported.run_id=r.id AND imported.style_id=(r.config_snapshot->>'style_id')::uuid AND imported.state='complete'
  JOIN photo.tasks t ON t.tenant_id=imported.tenant_id AND t.run_id=imported.run_id AND t.id=imported.task_id AND t.operation='style.import' AND t.operation_version='1' AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  JOIN photo.artifacts config ON config.tenant_id=t.tenant_id AND config.created_by_task_id=t.id AND config.output_role='style_candidate' AND config.state='ready' AND config.object_version IS NOT NULL AND config.object_version NOT IN('','null') AND (config.expires_at IS NULL OR config.expires_at>clock_timestamp()) AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',config.id::text,'sha256',config.sha256))
  JOIN photo.artifacts report ON report.tenant_id=t.tenant_id AND report.created_by_task_id=t.id AND report.output_role='aggregate_report' AND report.state='ready' AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp()) AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',report.id::text,'sha256',report.sha256))
  WHERE r.kind='style_import' AND r.status IN('queued','running','completed') AND r.config_snapshot ? 'style_id' AND NOT r.cancel_requested
 ) q LEFT JOIN photo.style_candidates existing$branch$;
BEGIN
 SELECT p.* INTO STRICT before FROM pg_proc p WHERE p.oid='photo.claim_style_candidate()'::regprocedure;
 IF encode(sha256(convert_to(before.prosrc,'UTF8')),'hex')<>'b9b3682e8648a7b7e6457bec8753bfbbf2b2a9d34d00ff5bb50ab9851a087b63'
 OR before.proowner<>'photo_fn_owner'::regrole OR NOT before.prosecdef
 OR before.prolang<>(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 OR ('search_path=pg_catalog, photo'=ANY(before.proconfig) OR 'search_path=pg_catalog,photo'=ANY(before.proconfig)) IS NOT TRUE
 THEN RAISE EXCEPTION 'STYLE_IMPORT_CANDIDATE_PREIMAGE_DRIFT';END IF;
 IF (length(before.prosrc)-length(replace(before.prosrc,needle,'')))/length(needle)<>1
 THEN RAISE EXCEPTION 'STYLE_IMPORT_CANDIDATE_ANCHOR_DRIFT';END IF;
 expected_source:=replace(before.prosrc,needle,replacement);
 IF replace(expected_source,replacement,needle) IS DISTINCT FROM before.prosrc
 THEN RAISE EXCEPTION 'STYLE_IMPORT_CANDIDATE_INVERSE_DRIFT';END IF;
 definition:=pg_get_functiondef(before.oid);EXECUTE replace(definition,needle,replacement);
 SELECT p.* INTO STRICT after FROM pg_proc p WHERE p.oid=before.oid;
 IF (to_jsonb(after)-'prosrc') IS DISTINCT FROM (to_jsonb(before)-'prosrc') OR after.prosrc IS DISTINCT FROM expected_source
 THEN RAISE EXCEPTION 'STYLE_IMPORT_CANDIDATE_POSTIMAGE_DRIFT';END IF;
END $migration$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02649 is forward-only';END $$;
-- +goose StatementEnd
