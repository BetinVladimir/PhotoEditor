-- +goose Up
-- Separate reusable prepare source proof from worker lease; only trusted save
-- and the worker wrapper may invoke this private predicate.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_prepare_source_ready(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof jsonb;frozen jsonb;snapshot jsonb;params jsonb;cfg jsonb;preset jsonb;item jsonb;
 v_project uuid;v_actor uuid;v_session uuid;v_snapshot uuid;raw text;node_count integer;v_ordinal integer;
BEGIN
 -- Assembly must additionally prove retained child exports under package locks;
 -- deny until its dedicated source gate is implemented.
 IF NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
  AND run_id=p_run AND operation='v2.delivery.prepare') THEN RETURN false;END IF;
 SELECT d.v30_delivery_proof,r.created_by INTO proof,v_actor FROM photo.backend_documents d
 JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_task AND d.purpose='task_params'
 AND d.artifact_id=p_params AND d.sha256=p_sha AND d.state='ready' FOR SHARE OF d;
 IF proof IS NULL OR (proof-'schema_version'-'project_id'-'auth_session_id'-'frozen_input_jcs'-'snapshot_jcs'-'configs_jcs')<>'{}'::jsonb
 OR proof->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(proof->'configs_jcs') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 v_project:=(proof->>'project_id')::uuid;v_session:=(proof->>'auth_session_id')::uuid;
 IF photo.v2_editor_authorized(p_tenant,v_project,v_actor,v_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.project_id=v_project) THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=v_project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 params:=p_json::jsonb;raw:=proof->>'frozen_input_jcs';
 IF raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576
 OR encode(sha256(convert_to(raw,'UTF8')),'hex') IS DISTINCT FROM params->'input'->>'sha256' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(params->'input'->>'artifact_id')::uuid
  AND a.sha256=params->'input'->>'sha256' AND a.media_type='application/json' AND a.bytes=octet_length(raw)) THEN RETURN false;END IF;
 frozen:=raw::jsonb;
 IF frozen->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(frozen->'presets') IS DISTINCT FROM 'array'
 OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'off' OR frozen->'policy'->>'metadata_policy' IS DISTINCT FROM 'strip'
 OR jsonb_array_length(frozen->'presets') NOT BETWEEN 1 AND 16
 OR jsonb_array_length(proof->'configs_jcs') IS DISTINCT FROM jsonb_array_length(frozen->'presets') THEN RETURN false;END IF;
 raw:=proof->>'snapshot_jcs';
 IF raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 8388608
 OR encode(sha256(convert_to(raw,'UTF8')),'hex') IS DISTINCT FROM frozen->'snapshot'->>'sha256' THEN RETURN false;END IF;
 snapshot:=raw::jsonb;v_snapshot:=(snapshot->>'snapshot_id')::uuid;
 IF snapshot->>'snapshot_digest' IS DISTINCT FROM frozen->>'snapshot_digest'
 OR photo.v25_lens_snapshot_ready(p_tenant,v_project,v_snapshot,v_actor,v_session) IS DISTINCT FROM true
 OR jsonb_typeof(snapshot->'items') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT item_count INTO node_count FROM photo.snapshots WHERE tenant_id=p_tenant AND id=v_snapshot FOR SHARE;
 IF node_count IS NULL OR jsonb_array_length(snapshot->'items') IS DISTINCT FROM node_count
 OR NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND input_snapshot_id=v_snapshot) THEN RETURN false;END IF;
 FOR item,v_ordinal IN SELECT value,(ordinality-1)::integer FROM jsonb_array_elements(snapshot->'items') WITH ORDINALITY LOOP
  IF item->>'ordinal' IS DISTINCT FROM v_ordinal::text OR NOT EXISTS(SELECT 1 FROM photo.snapshot_items si
   JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id AND v.review_state='accepted'
   WHERE si.tenant_id=p_tenant AND si.snapshot_id=v_snapshot AND si.ordinal=v_ordinal
   AND si.asset_version_id::text=item->'binding'->>'asset_version_id' AND si.artifact_id::text=item->'binding'->'source'->>'artifact_id'
   AND si.sha256=item->'binding'->'source'->>'sha256') THEN RETURN false;END IF;
  IF photo.v26_render_recipe_scope(p_tenant,v_project,(item->'binding'->>'asset_version_id')::uuid,
   (item->'recipe'->>'artifact_id')::uuid,item->'recipe'->>'sha256',v_actor,v_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND i.contract='InputSnapshot' AND i.artifact_id=(frozen->'snapshot'->>'artifact_id')::uuid AND i.sha256=frozen->'snapshot'->>'sha256') THEN RETURN false;END IF;
 FOR preset IN SELECT value FROM jsonb_array_elements(frozen->'presets') LOOP
  SELECT value INTO cfg FROM jsonb_array_elements(proof->'configs_jcs') WHERE value->>'preset_id'=preset->>'preset_id';
  IF cfg IS NULL OR (SELECT count(*) FROM jsonb_array_elements(proof->'configs_jcs') WHERE value->>'preset_id'=preset->>'preset_id')<>1 THEN RETURN false;END IF;
  raw:=cfg->>'config_jcs';
  IF raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576
   OR encode(sha256(convert_to(raw,'UTF8')),'hex') IS DISTINCT FROM preset->>'preset_sha'
   OR preset->>'preset_sha' IS DISTINCT FROM preset->'config'->>'sha256'
   OR (raw::jsonb)->>'metadata_policy' IS DISTINCT FROM 'strip'
   OR (raw::jsonb)->'watermark' IS NOT NULL THEN RETURN false;END IF;
  -- Watermark nested source authority is not yet supplied. Refuse, never omit it.
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=(preset->'config'->>'artifact_id')::uuid
   AND i.sha256=preset->'config'->>'sha256' AND a.media_type='application/json' AND a.bytes=octet_length(raw)) THEN RETURN false;END IF;
 END LOOP;
 RETURN photo.v2_editor_authorized(p_tenant,v_project,v_actor,v_session)
 AND photo.v25_lens_snapshot_ready(p_tenant,v_project,v_snapshot,v_actor,v_session);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_prepare_source_ready(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_prepare_source_ready(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
CREATE OR REPLACE FUNCTION photo.v30_delivery_source_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v30_delivery_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.v30_delivery_prepare_source_ready(p_tenant,p_run,p_task,p_params,p_sha,p_json);
END $$;

CREATE FUNCTION photo.save_v30_delivery_prepare_proof(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,
 p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;d photo.backend_documents%ROWTYPE;params jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_project IS NULL OR p_run IS NULL OR p_task IS NULL OR p_params IS NULL
 OR p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha
 OR p_proof IS NULL OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR pg_column_size(p_proof)>16777216
 OR p_proof->>'project_id' IS DISTINCT FROM p_project::text OR p_proof->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 params:=p_json::jsonb;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT count(*) FROM jsonb_object_keys(params))<>2 OR jsonb_typeof(params->'input') IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(params->'input'))<>2 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task
 AND purpose='task_params' AND artifact_id=p_params AND sha256=p_sha AND bytes=octet_length(p_json) AND state='ready' FOR UPDATE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.created_by IS DISTINCT FROM p_actor OR r.cancel_requested
 OR t.id IS NULL OR t.operation IS DISTINCT FROM 'v2.delivery.prepare' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.params_artifact_id IS DISTINCT FROM p_params OR t.cancel_requested
 OR d.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.project_id=p_project) THEN RETURN false;END IF;
 IF d.v30_delivery_proof IS NOT NULL THEN
  RETURN d.v30_delivery_proof=p_proof AND photo.v30_delivery_prepare_source_ready(p_tenant,p_run,p_task,p_params,p_sha,p_json);
 END IF;
 IF r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0 THEN RETURN false;END IF;
 UPDATE photo.backend_documents SET v30_delivery_proof=p_proof WHERE id=d.id;
 IF photo.v30_delivery_prepare_source_ready(p_tenant,p_run,p_task,p_params,p_sha,p_json) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'Delivery prepare proof refused' USING ERRCODE='23514';
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v30_delivery_prepare_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v30_delivery_prepare_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.save_v30_delivery_prepare_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
