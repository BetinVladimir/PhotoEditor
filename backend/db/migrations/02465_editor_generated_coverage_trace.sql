-- +goose Up
ALTER TABLE photo.editor_effect_builds ADD COLUMN seal_coverage_trace jsonb;
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
 VALUES('editor.effects_seal','1','generated_pixels','["application/octet-stream"]'::jsonb,800008192,1);
-- +goose StatementBegin
ALTER FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 RENAME TO editor_effect_seal_worker_origin_ready;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_worker_origin_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM photo_worker;
DO $fix$
DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('photo.editor_effect_seal_worker_origin_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure) INTO definition;
 IF position('OR b.seal_generative_requested IS DISTINCT FROM (b.seal_generated_mask IS NOT NULL)' IN definition)=0 THEN
  RAISE EXCEPTION 'EDITOR_SEAL_ORIGIN_CONTRACT_DRIFT';
 END IF;
 definition:=replace(definition,'OR b.seal_generative_requested IS DISTINCT FROM (b.seal_generated_mask IS NOT NULL)',
  'OR (jsonb_array_length(coalesce(body->''coverage_trace'',''[]''::jsonb))=0 AND b.seal_generative_requested IS DISTINCT FROM (b.seal_generated_mask IS NOT NULL))');
 EXECUTE definition;
END $fix$;
CREATE FUNCTION photo.editor_effect_seal_worker_ready(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;body jsonb;trace jsonb;entry jsonb;
 t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;n photo.apply_nodes%ROWTYPE;previous jsonb;
 seen uuid[]:='{}';count_keys integer;
BEGIN
 IF photo.editor_effect_seal_worker_origin_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 body:=p_json::jsonb;trace:=coalesce(body->'coverage_trace','[]'::jsonb);
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=(body->>'build_run_id')::uuid FOR SHARE;
 IF jsonb_typeof(trace)<>'array' OR jsonb_array_length(trace)>128 OR trace IS DISTINCT FROM coalesce(b.seal_coverage_trace,'[]'::jsonb) THEN RETURN false;END IF;
 IF jsonb_array_length(trace)=0 THEN RETURN true;END IF;
 previous:=jsonb_build_object('artifact_id',coalesce(b.effect_source_artifact_id,b.source_artifact_id),
  'sha256',coalesce(b.effect_source_sha,b.source_sha));
 FOR entry IN SELECT value FROM jsonb_array_elements(trace) LOOP
  SELECT count(*) INTO count_keys FROM jsonb_object_keys(entry);
  IF jsonb_typeof(entry)<>'object' OR count_keys<>8 OR NOT(entry ?& ARRAY['task_id','result_id','attempt','fence','operation','source','output','report'])
   OR entry->'source' IS DISTINCT FROM previous OR (entry->>'task_id')::uuid=ANY(seen) THEN RETURN false;END IF;
  seen:=array_append(seen,(entry->>'task_id')::uuid);
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=b.run_id AND id=(entry->>'task_id')::uuid;
  SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=(entry->>'result_id')::uuid AND task_id=t.id;
  SELECT * INTO n FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=b.run_id AND id=t.id;
  IF t.id IS NULL OR z.id IS NULL OR n.id IS NULL OR t.state<>'succeeded' OR t.cancel_requested
   OR t.operation IS DISTINCT FROM entry->>'operation' OR t.canonical_result_id IS DISTINCT FROM z.id
   OR t.attempt IS DISTINCT FROM (entry->>'attempt')::integer OR t.fencing_token IS DISTINCT FROM (entry->>'fence')::bigint
   OR z.attempt IS DISTINCT FROM t.attempt OR z.fencing_token IS DISTINCT FROM t.fencing_token
   OR n.operation IS DISTINCT FROM t.operation OR n.params_sha256 IS DISTINCT FROM (SELECT sha256 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id)
   OR n.params->'image' IS DISTINCT FROM entry->'source'
   OR NOT t.inputs @> jsonb_build_array(entry->'source') OR NOT z.outputs @> jsonb_build_array(entry->'output')
   OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(entry#>>'{output,artifact_id}')::uuid
    AND a.sha256=entry#>>'{output,sha256}' AND a.output_role='working_image' AND a.created_by_task_id=t.id
    AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
  IF entry->>'operation' IN('image.generate','image.inpaint','image.outpaint') AND entry->'report'='null'::jsonb THEN RETURN false;END IF;
  IF entry->'report'<>'null'::jsonb AND (NOT z.outputs @> jsonb_build_array(entry->'report') OR NOT EXISTS(
   SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(entry#>>'{report,artifact_id}')::uuid AND a.sha256=entry#>>'{report,sha256}'
    AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token AND a.state='ready'
    AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND a.output_role=CASE WHEN t.operation='image.inpaint' THEN 'inpaint_report' ELSE 'generation_report' END)) THEN RETURN false;END IF;
  previous:=entry->'output';
 END LOOP;
 RETURN previous=jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
