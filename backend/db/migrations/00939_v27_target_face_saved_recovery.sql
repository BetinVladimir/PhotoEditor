-- +goose Up
-- Scoped saved target locators and exact transport recovery; never repair/queue.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_target_face_saved(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_ordinal integer,p_manifest_sha text,p_operation text)
RETURNS jsonb LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.runs%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c jsonb;e jsonb;namespace uuid;child_run uuid;child_task uuid;params_sha text;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN('face.landmarks','face.parse') THEN RETURN NULL;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999 THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference');
 IF d.id IS NULL OR NOT d.graph_sealed OR d.cancel_requested OR d.v27_initial_manual_selection IS NOT NULL
 OR d.created_by IS DISTINCT FROM p_actor OR d.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR d.config_snapshot->'v27_detector_prepare'->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR d.v27_target_manifest_sha IS DISTINCT FROM p_manifest_sha OR d.v27_target_manifest IS NULL THEN RETURN NULL;END IF;
 e:=d.v27_target_manifest::jsonb->'entries'->p_ordinal;
 IF e IS NULL OR e->'reuse_reference' IS DISTINCT FROM 'false'::jsonb THEN RETURN NULL;END IF;
 namespace:=photo.semantic_uuid_v5(p_parent,'semantic.target.prepare:1:'||(d.config_snapshot->'v27_detector_prepare'->'snapshot'->>'sha256')||':'||p_ordinal||':'||encode(public.digest(convert_to(photo.v27_target_manifest_json_text(e->'binding'),'UTF8'),'sha256'),'hex'));
 IF e->>'namespace' IS DISTINCT FROM namespace::text THEN RETURN NULL;END IF;
 child_run:=photo.semantic_uuid_v5(namespace,'semantic.'||p_operation||':reference');child_task:=photo.semantic_uuid_v5(photo.semantic_uuid_v5(child_run,'workflow'),p_operation||':0');
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=child_run;
 IF NOT FOUND THEN
  IF EXISTS(SELECT 1 FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND route='V2.Admit' AND key='target-face-prep:'||child_run::text) THEN RETURN NULL;END IF;
  RETURN jsonb_build_object('state','absent');
 END IF;
 c:=r.config_snapshot->'v27_target_face_prepare';
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=child_run AND id=child_task;
 IF r.kind<>'v2' OR r.created_by IS DISTINCT FROM p_actor OR r.cancel_requested OR t.id IS NULL OR t.cancel_requested
 OR r.input_snapshot_id IS DISTINCT FROM d.input_snapshot_id OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR c->'schema_version' IS DISTINCT FROM '1'::jsonb OR c->>'parent_run_id' IS DISTINCT FROM p_parent::text
 OR c->>'auth_session_id' IS DISTINCT FROM p_session::text OR c->'source' IS DISTINCT FROM e->'binding'
 OR c->>'target_namespace' IS DISTINCT FROM namespace::text OR c->'target_ordinal' IS DISTINCT FROM to_jsonb(p_ordinal)
 OR c->>'target_stage_id' IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'stage:'||p_operation)::text
 OR c->>'manifest_sha' IS DISTINCT FROM p_manifest_sha OR c->'request_sha' IS DISTINCT FROM d.config_snapshot->'v27_detector_prepare'->'request_sha'
 OR c->'intent' IS DISTINCT FROM d.config_snapshot->'v27_detector_prepare'->'intent' OR c->'snapshot' IS DISTINCT FROM d.config_snapshot->'v27_detector_prepare'->'snapshot'
 OR t.operation IS DISTINCT FROM p_operation OR c->>'operation' IS DISTINCT FROM p_operation OR t.operation_version<>'1' OR t.resource_class<>'gpu' OR t.node_key IS DISTINCT FROM p_operation||':0'
 OR c->>'task_id' IS DISTINCT FROM t.id::text OR c->>'input_digest' IS DISTINCT FROM t.input_digest
 OR c->'params'->>'artifact_id' IS DISTINCT FROM t.params_artifact_id::text
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id)<>1 THEN RETURN NULL;END IF;
 SELECT sha256 INTO params_sha FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id AND state='ready';
 IF params_sha IS NULL OR c->'params'->>'sha256' IS DISTINCT FROM params_sha THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('state','saved','run_id',r.id,'task_id',t.id,'config',r.config_snapshot,'config_sha',r.config_hash,'input_digest',t.input_digest,
 'profile',t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha','params',c->'params','run_state',r.status,'task_state',t.state,'sealed',r.graph_sealed);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.semantic_target_face_saved(uuid,uuid,uuid,uuid,uuid,integer,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_face_saved(uuid,uuid,uuid,uuid,uuid,integer,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_face_saved(uuid,uuid,uuid,uuid,uuid,integer,text,text) TO photo_api;
CREATE FUNCTION photo.semantic_target_face_registered_transport(p_tenant uuid,p_run uuid,p_task uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;project uuid;params_sha text;rows jsonb;chunks jsonb;binding jsonb;state text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 SELECT project_id INTO project FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR t.id IS NULL OR r.created_by IS DISTINCT FROM p_actor OR t.operation NOT IN('face.landmarks','face.parse')
 OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(t.execution_profile->'v2_input_manifest') IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 SELECT a.sha256 INTO params_sha FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id AND a.state='ready';
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal),'[]'::jsonb) INTO rows
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal<1000000;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) ORDER BY i.ordinal),'[]'::jsonb) INTO chunks
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=1000000 AND i.contract='InputClosureChunk';
 binding:=jsonb_build_object('schema_version',1,'task_id',t.id,'params',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',params_sha),'input_digest',t.input_digest,
 'profile',t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha',
 'transport',jsonb_build_object('schema_version',1,'rows',rows,'manifest',t.execution_profile->'v2_input_manifest','chunks',chunks,'direct',t.inputs-0,'count',(t.execution_profile->>'v2_entry_count')::integer,'entries_sha',t.execution_profile->>'v2_entries_sha'));
 IF r.graph_sealed THEN
  state:=photo.semantic_target_face_sealed_registration_binding_state(p_tenant,p_run,t.operation,binding);
 ELSE
  state:=photo.semantic_target_face_registration_binding_state(p_tenant,p_run,t.operation,binding);
 END IF;
 IF state IS DISTINCT FROM 'replayed' THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'task_id',p_task,'manifest',binding->'transport'->'manifest','chunks',chunks,'count',binding->'transport'->'count','entries_sha',binding->'transport'->'entries_sha');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.semantic_target_face_registered_transport(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_face_registered_transport(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_face_registered_transport(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
