-- +goose Up
-- Exact target canonical result discovery and current locked consumer authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_target_face_canonical_outputs(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_config_sha text)
RETURNS jsonb LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;c jsonb;d photo.runs%ROWTYPE;e jsonb;ordinal integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 c:=r.config_snapshot->'v27_target_face_prepare';
 IF coalesce(c->>'operation','') NOT IN('face.landmarks','face.parse') THEN RETURN NULL;END IF;
 IF r.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.status NOT IN('running','succeeded')
 OR r.created_by IS DISTINCT FROM p_actor OR r.config_hash IS DISTINCT FROM p_config_sha
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text OR c->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR c->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5((c->>'parent_run_id')::uuid,'semantic.face.detect:reference');
 IF d.id IS NULL OR NOT d.graph_sealed OR d.cancel_requested OR d.status NOT IN('queued','running','succeeded')
 OR d.v27_initial_manual_selection IS NOT NULL OR d.v27_target_manifest_sha IS DISTINCT FROM c->>'manifest_sha'
 OR d.v27_target_manifest IS NULL OR d.created_by IS DISTINCT FROM p_actor THEN RETURN NULL;END IF;
 ordinal:=(c->>'target_ordinal')::integer;e:=d.v27_target_manifest::jsonb->'entries'->ordinal;
 IF ordinal NOT BETWEEN 0 AND 4999 OR e IS NULL OR e->'reuse_reference' IS DISTINCT FROM 'false'::jsonb
 OR e->'binding' IS DISTINCT FROM c->'source' OR e->'namespace' IS DISTINCT FROM c->'target_namespace'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((e->>'namespace')::uuid,'semantic.'||(c->>'operation')||':reference') THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=photo.semantic_uuid_v5(photo.semantic_uuid_v5(p_run,'workflow'),(c->>'operation')||':0');
 IF t.id IS NULL OR t.state<>'succeeded' OR t.cancel_requested OR t.operation IS DISTINCT FROM c->>'operation' OR t.operation_version<>'1'
 OR t.id::text IS DISTINCT FROM c->>'task_id' OR t.input_digest IS DISTINCT FROM c->>'input_digest' THEN RETURN NULL;END IF;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=t.canonical_result_id AND attempt=t.attempt AND fencing_token=t.fencing_token;
 IF result.id IS NULL OR jsonb_typeof(result.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(result.outputs)<>(CASE WHEN c->>'operation'='face.landmarks' THEN 1 ELSE 2 END) THEN RETURN NULL;END IF;
 IF photo.semantic_target_face_result_ready(p_tenant,t.id,t.attempt,t.fencing_token,result.outputs) IS NOT TRUE THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('result_id',result.id,'outputs',result.outputs);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.semantic_target_face_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_face_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_face_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
CREATE FUNCTION photo.lock_semantic_target_face_result(p_tenant uuid,p_run uuid,p_packet jsonb,p_result uuid,p_outputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;actual jsonb;t photo.tasks%ROWTYPE;r photo.task_results%ROWTYPE;
BEGIN
 IF photo.lock_semantic_target_face_sealed_evidence(p_tenant,p_run,p_packet) IS NOT TRUE THEN RETURN false;END IF;
 e:=p_packet->'face';
 actual:=photo.semantic_target_face_canonical_outputs(p_tenant,(e->>'project_id')::uuid,(e->>'actor_id')::uuid,(e->>'session_id')::uuid,p_run,
 encode(public.digest(convert_to(e->>'config_text','UTF8'),'sha256'),'hex'));
 IF actual IS NULL OR actual->>'result_id' IS DISTINCT FROM p_result::text OR actual->'outputs' IS DISTINCT FROM p_outputs THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(e->'registration'->>'task_id')::uuid FOR UPDATE;
 SELECT * INTO r FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=p_result FOR SHARE;
 RETURN t.state='succeeded' AND t.canonical_result_id=p_result AND r.attempt=t.attempt AND r.fencing_token=t.fencing_token
 AND r.outputs=actual->'outputs' AND photo.semantic_target_face_result_ready(p_tenant,t.id,t.attempt,t.fencing_token,r.outputs);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_face_result(uuid,uuid,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_face_result(uuid,uuid,jsonb,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_target_face_result(uuid,uuid,jsonb,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
