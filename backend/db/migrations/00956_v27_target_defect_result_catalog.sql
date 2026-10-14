-- +goose Up
-- Exact target canonical discovery and six immutable object pins; no new DML grants.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_target_defect_result_catalog(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid)
RETURNS TABLE(result_id uuid,outputs jsonb,params jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;z photo.task_results%ROWTYPE;c jsonb;dependency jsonb;producer photo.runs%ROWTYPE;skin record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR p_parent IS NULL OR coalesce(p_request_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 c:=r.config_snapshot->'v27_target_defect_prepare';
 IF t.id IS NULL OR r.id IS NULL OR t.state<>'succeeded' OR r.status NOT IN('running','completed')
 OR r.created_by IS DISTINCT FROM p_actor OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR c->>'parent_run_id' IS DISTINCT FROM p_parent::text OR c->>'request_sha' IS DISTINCT FROM p_request_sha
 OR c->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR photo.lock_v27_target_defect_sealed_graph_scope(p_tenant,r.id,t.id) IS NOT TRUE THEN RETURN;END IF;
 SELECT * INTO producer FROM photo.runs WHERE tenant_id=p_tenant AND id=(c->>'person_run_id')::uuid;
 dependency:=photo.semantic_target_person_canonical_outputs(p_tenant,p_project,p_actor,p_session,producer.id,producer.config_hash);
 IF dependency IS NULL OR dependency->'result_id' IS DISTINCT FROM c->'person_result_id' OR dependency->'outputs' IS DISTINCT FROM c->'person_outputs' THEN RETURN;END IF;
 SELECT * INTO producer FROM photo.runs WHERE tenant_id=p_tenant AND id=(c->>'landmarks_run_id')::uuid;
 dependency:=photo.semantic_target_face_canonical_outputs(p_tenant,p_project,p_actor,p_session,producer.id,producer.config_hash);
 IF dependency IS NULL OR dependency->'result_id' IS DISTINCT FROM c->'landmarks_result_id' OR dependency->'outputs' IS DISTINCT FROM c->'landmarks_outputs'
 OR producer.config_snapshot->'v27_target_face_prepare'->'params' IS DISTINCT FROM c->'landmarks_params' THEN RETURN;END IF;
 SELECT * INTO skin FROM photo.semantic_target_skin_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,(c->>'skin_task_id')::uuid);
 IF NOT FOUND OR to_jsonb(skin.result_id) IS DISTINCT FROM c->'skin_result_id' OR skin.outputs IS DISTINCT FROM c->'skin_outputs' OR skin.params IS DISTINCT FROM c->'skin_params' THEN RETURN;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id AND attempt=t.attempt AND fencing_token=t.fencing_token;
 IF z.id IS NULL OR photo.semantic_target_defect_completed_output_ready(p_tenant,t.id,t.attempt,t.fencing_token,z.outputs) IS NOT TRUE THEN RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_defect_version_params_object(p_tenant,p_project,p_actor,p_session,
 (c->'source'->>'asset_version_id')::uuid,(c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',
 (c->>'params_document')::uuid,(c->'params'->>'artifact_id')::uuid,c->'params'->>'sha256')) THEN RETURN;END IF;
 RETURN QUERY SELECT z.id,z.outputs,c->'params';
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_target_defect_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_defect_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_defect_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
CREATE FUNCTION photo.semantic_target_defect_result_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_result uuid)
RETURNS TABLE(ordinal integer,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE found_result record;refs jsonb;ref jsonb;i integer;
BEGIN
 SELECT * INTO found_result FROM photo.semantic_target_defect_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND OR found_result.result_id IS DISTINCT FROM p_result THEN RETURN;END IF;
 refs:=found_result.outputs||jsonb_build_array(found_result.params);
 FOR i IN 0..5 LOOP
  ref:=refs->i;
  RETURN QUERY SELECT i,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
  FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes>0 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 END LOOP;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_target_defect_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_defect_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_defect_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
