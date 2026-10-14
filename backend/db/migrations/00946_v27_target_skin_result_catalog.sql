-- +goose Up
-- Exact parent-owned CPU skin result discovery for downstream native preparation.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_target_skin_result_catalog(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid)
RETURNS TABLE(result_id uuid,outputs jsonb,params jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;z photo.task_results%ROWTYPE;p photo.artifacts%ROWTYPE;c jsonb;ref jsonb;dependency jsonb;pr photo.runs%ROWTYPE;roles text[];media text[];i integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR p_parent IS NULL OR coalesce(p_request_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.state<>'succeeded' OR t.cancel_requested OR t.operation IS DISTINCT FROM 'mask.face_skin' OR t.operation_version<>'1'
 OR t.resource_class<>'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
 OR r.status<>'running' THEN RETURN;END IF;
 c:=r.config_snapshot->'v27_target_skin_prepare';
 IF c->'schema_version' IS DISTINCT FROM '1'::jsonb OR c->>'parent_run_id' IS DISTINCT FROM p_parent::text
 OR c->>'request_sha' IS DISTINCT FROM p_request_sha OR c->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR c->>'task_id' IS DISTINCT FROM t.id::text OR c->>'input_digest' IS DISTINCT FROM t.input_digest
 OR r.id IS DISTINCT FROM photo.semantic_uuid_v5((c->>'target_namespace')::uuid,'semantic.'||t.operation||':reference')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND project_id=p_project AND workflow_type='semantic' AND renderer_version='v2.2') THEN RETURN;END IF;
 IF c->>'operation' IS DISTINCT FROM t.operation OR t.attempt<1 OR t.fencing_token<1 THEN RETURN;END IF;
 SELECT * INTO pr FROM photo.runs WHERE tenant_id=p_tenant AND id=(c->>'parser_run_id')::uuid;
 dependency:=photo.semantic_target_face_canonical_outputs(p_tenant,p_project,p_actor,p_session,pr.id,pr.config_hash);
 IF pr.id IS NULL OR pr.status<>'completed' OR dependency IS NULL
 OR c->'parser_result_id' IS DISTINCT FROM dependency->'result_id' OR c->'parser_outputs' IS DISTINCT FROM dependency->'outputs'
 OR c->'parser_params' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'params'
 OR c->>'parser_task_id' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->>'task_id'
 OR c->>'parser_run_id' IS DISTINCT FROM photo.semantic_uuid_v5((c->>'target_namespace')::uuid,'semantic.face.parse:reference')::text THEN RETURN;END IF;
 IF c->'source' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'source'
 OR c->'target_namespace' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'target_namespace'
 OR c->'target_ordinal' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'target_ordinal'
 OR c->'manifest_sha' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'manifest_sha'
 OR c->'parent_run_id' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'parent_run_id'
 OR c->'request_sha' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'request_sha'
 OR c->'intent' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'intent'
 OR c->'snapshot' IS DISTINCT FROM pr.config_snapshot->'v27_target_face_prepare'->'snapshot'
 OR c->>'target_stage_id' IS DISTINCT FROM photo.semantic_uuid_v5((c->>'target_namespace')::uuid,'stage:mask.face_skin')::text
 OR c->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5((c->>'target_namespace')::uuid,'semantic.mask.face_skin.params:reference')::text
 OR t.id IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(r.id,'workflow'),'mask.face_skin:0') THEN RETURN;END IF;
 roles:=ARRAY['skin_mask','skin_mask_png','skin_mask_ref'];media:=ARRAY['application/octet-stream','image/png','application/json'];
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id AND attempt=t.attempt AND fencing_token=t.fencing_token;
 IF NOT FOUND OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(z.outputs)<>cardinality(roles) OR octet_length(z.outputs::text)>4096 THEN RETURN;END IF;
 SELECT * INTO p FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id;
 IF NOT FOUND OR c->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p.id,'sha256',p.sha256) THEN RETURN;END IF;
 FOR i IN 0..cardinality(roles)-1 LOOP
  ref:=z.outputs->i;
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}' OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR (ref->>'artifact_id')::uuid=t.params_artifact_id
  OR EXISTS(SELECT 1 FROM jsonb_array_elements(z.outputs) WITH ORDINALITY item(value,n) WHERE n<>i+1 AND value->>'artifact_id'=ref->>'artifact_id') THEN RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
 WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256' AND a.output_role=roles[i+1] AND a.media_type=media[i+1]
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND u.task_id=t.id AND u.attempt=t.attempt AND u.fence=t.fencing_token AND u.state='ready' AND u.completed_at IS NOT NULL
 AND u.role=a.output_role AND u.bytes=a.bytes AND u.sha256=a.sha256 AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version AND u.media_type=a.media_type) THEN RETURN;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_defect_version_params_object(p_tenant,p_project,p_actor,p_session,
 (c->'source'->>'asset_version_id')::uuid,(c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',
 (c->>'params_document')::uuid,p.id,p.sha256)) THEN RETURN;END IF;
 RETURN QUERY SELECT z.id,z.outputs,jsonb_build_object('artifact_id',p.id,'sha256',p.sha256);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_target_skin_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_skin_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_skin_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_target_skin_result_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_result uuid)
RETURNS TABLE(ordinal integer,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE found_result record;refs jsonb;ref jsonb;i integer;
BEGIN
 SELECT * INTO found_result FROM photo.semantic_target_skin_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND OR found_result.result_id IS DISTINCT FROM p_result THEN RETURN;END IF;
 refs:=found_result.outputs||jsonb_build_array(found_result.params);
 FOR i IN 0..3 LOOP
  ref:=refs->i;
  RETURN QUERY SELECT i,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
  FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes>0 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 END LOOP;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_target_skin_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_skin_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_skin_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
