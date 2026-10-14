-- +goose Up
-- Saved proof uses existing backend_documents lifecycle/purge ownership.
-- No execution may infer frozen source authority from a live lease alone.
ALTER TABLE photo.backend_documents ADD COLUMN v30_delivery_proof jsonb;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v30_delivery_proof_size
 CHECK(v30_delivery_proof IS NULL OR (purpose='task_params' AND jsonb_typeof(v30_delivery_proof)='object'
 AND pg_column_size(v30_delivery_proof)<=16777216));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v30_delivery_proof() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v30_delivery_proof IS NOT NULL AND current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'Delivery proof requires trusted admission' USING ERRCODE='42501';END IF;
 ELSIF NEW.v30_delivery_proof IS DISTINCT FROM OLD.v30_delivery_proof THEN
  IF OLD.v30_delivery_proof IS NOT NULL OR current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'Delivery proof is immutable' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v30_delivery_proof() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v30_delivery_proof() FROM PUBLIC;
CREATE TRIGGER backend_documents_v30_delivery_proof_guard BEFORE INSERT OR UPDATE ON photo.backend_documents
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v30_delivery_proof();

CREATE FUNCTION photo.v30_delivery_source_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof jsonb;frozen jsonb;snapshot jsonb;params jsonb;cfg jsonb;preset jsonb;item jsonb;
 v_project uuid;v_actor uuid;v_session uuid;v_snapshot uuid;raw text;node_count integer;v_ordinal integer;
BEGIN
 IF photo.v30_delivery_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
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
ALTER FUNCTION photo.v30_delivery_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
