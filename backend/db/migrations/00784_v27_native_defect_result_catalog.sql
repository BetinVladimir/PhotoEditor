-- +goose Up
-- Exact parent-owned CPU native defect result discovery for downstream native preparation.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_defect_result_catalog(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid)
RETURNS TABLE(result_id uuid,outputs jsonb,params jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;z photo.task_results%ROWTYPE;p photo.artifacts%ROWTYPE;c jsonb;ref jsonb;dependency record;roles text[];media text[];i integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR p_parent IS NULL OR coalesce(p_request_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.state<>'succeeded' OR t.cancel_requested OR t.operation IS DISTINCT FROM 'defects.propose' OR t.operation_version<>'1'
 OR t.resource_class<>'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
 OR r.status NOT IN('running','waiting_review','completed') THEN RETURN;END IF;
 c:=r.config_snapshot->'v27_defect_native_prepare';
 IF c->'schema_version' IS DISTINCT FROM '1'::jsonb OR c->>'parent_run_id' IS DISTINCT FROM p_parent::text
 OR c->>'request_sha' IS DISTINCT FROM p_request_sha OR c->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR c->>'task_id' IS DISTINCT FROM t.id::text OR c->>'input_digest' IS DISTINCT FROM t.input_digest
 OR r.id IS DISTINCT FROM photo.semantic_uuid_v5(p_parent,'semantic.defect.prepare:reference')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND project_id=p_project AND workflow_type='semantic' AND renderer_version='v2.2') THEN RETURN;END IF;
 IF c->>'operation' IS DISTINCT FROM t.operation OR t.attempt<1 OR t.fencing_token<1 THEN RETURN;END IF;
 SELECT * INTO dependency FROM photo.semantic_native_face_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,(c->>'landmarks_task_id')::uuid);
 IF NOT FOUND OR c->'landmarks_result_id' IS DISTINCT FROM to_jsonb(dependency.result_id)
 OR c->'landmarks_outputs' IS DISTINCT FROM dependency.outputs OR c->'landmarks_params' IS DISTINCT FROM dependency.params
 OR c->>'landmarks_run_id' IS DISTINCT FROM photo.semantic_uuid_v5(p_parent,'semantic.face.landmarks:reference')::text
 OR NOT EXISTS(SELECT 1 FROM photo.runs original WHERE original.tenant_id=p_tenant AND original.id=photo.semantic_uuid_v5(p_parent,'semantic.face.landmarks:reference') AND original.config_snapshot->'v27_face_prepare'->'source'=c->'source') THEN RETURN;END IF;
 SELECT * INTO dependency FROM photo.semantic_native_skin_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,(c->>'skin_task_id')::uuid);
 IF NOT FOUND OR c->'skin_result_id' IS DISTINCT FROM to_jsonb(dependency.result_id)
 OR c->'skin_outputs' IS DISTINCT FROM dependency.outputs OR c->'skin_params' IS DISTINCT FROM dependency.params
 OR c->>'skin_run_id' IS DISTINCT FROM photo.semantic_uuid_v5(p_parent,'semantic.mask.face_skin:reference')::text
 OR NOT EXISTS(SELECT 1 FROM photo.runs original WHERE original.tenant_id=p_tenant AND original.id=photo.semantic_uuid_v5(p_parent,'semantic.mask.face_skin:reference') AND original.config_snapshot->'v27_skin_prepare'->'source'=c->'source') THEN RETURN;END IF;
 roles:=ARRAY['defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report'];media:=ARRAY['application/octet-stream','application/octet-stream','image/png','application/json','application/json'];
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
ALTER FUNCTION photo.semantic_native_defect_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_defect_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_defect_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
