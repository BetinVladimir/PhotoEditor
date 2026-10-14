-- +goose Up
-- Target-only graph identity and transport replay. Never seals or queues.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v27_target_detector_graph_scope(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;d photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c jsonb;n jsonb;m jsonb;e jsonb;
 parent uuid;detector uuid;project uuid;session_id uuid;namespace uuid;ordinal integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT FOUND OR jsonb_typeof(r.config_snapshot->'v27_target_detector_prepare') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_target_detector_prepare';parent:=(c->>'parent_run_id')::uuid;
 detector:=photo.semantic_uuid_v5(parent,'semantic.face.detect:reference');
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=parent FOR SHARE;
 SELECT * INTO d FROM photo.runs WHERE tenant_id=p_tenant AND id=detector FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF d.id IS NULL OR r.id IS NULL OR t.id IS NULL OR d.kind<>'v2' OR NOT d.graph_sealed OR d.cancel_requested
 OR d.status NOT IN('queued','running','succeeded') OR d.v27_initial_manual_selection IS NOT NULL
 OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR t.state<>'blocked' OR t.cancel_requested OR t.attempt<>0 OR t.canonical_result_id IS NOT NULL
 OR t.operation<>'face.detect' OR t.operation_version<>'1' OR t.resource_class<>'gpu' OR t.node_key<>'face.detect:0'
 OR p_task IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(p_run,'workflow'),'face.detect:0') THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_target_detector_prepare';n:=d.config_snapshot->'v27_detector_prepare';
 project:=(d.config_snapshot->>'project_id')::uuid;session_id:=(n->>'auth_session_id')::uuid;
 IF c->'schema_version' IS DISTINCT FROM '1'::jsonb OR n->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR r.created_by IS DISTINCT FROM d.created_by OR r.input_snapshot_id IS DISTINCT FROM d.input_snapshot_id
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM project::text OR c->>'parent_run_id' IS DISTINCT FROM parent::text
 OR n->>'parent_run_id' IS DISTINCT FROM parent::text OR c->'auth_session_id' IS DISTINCT FROM n->'auth_session_id'
 OR c->'request_sha' IS DISTINCT FROM n->'request_sha' OR c->'intent' IS DISTINCT FROM n->'intent' OR c->'snapshot' IS DISTINCT FROM n->'snapshot'
 OR photo.v2_editor_authorized(p_tenant,project,r.created_by,session_id) IS NOT TRUE
 OR c->>'manifest_sha' IS DISTINCT FROM d.v27_target_manifest_sha OR d.v27_target_manifest IS NULL
 OR encode(public.digest(convert_to(d.v27_target_manifest,'UTF8'),'sha256'),'hex') IS DISTINCT FROM d.v27_target_manifest_sha
 OR jsonb_typeof(c->'target_ordinal') IS DISTINCT FROM 'number' OR c->>'target_ordinal' !~ '^(0|[1-9][0-9]*)$' THEN RETURN false;END IF;
 ordinal:=(c->>'target_ordinal')::integer;m:=d.v27_target_manifest::jsonb;
 IF jsonb_typeof(m->'entries') IS DISTINCT FROM 'array' OR ordinal NOT BETWEEN 0 AND 4999 OR ordinal>=jsonb_array_length(m->'entries')
 OR (m-'entries') IS DISTINCT FROM jsonb_build_object('schema_version',1,'tenant_id',p_tenant,'project_id',project,'actor_id',d.created_by,
 'session_id',session_id,'parent_run_id',parent,'request_sha',n->'request_sha','intent',n->'intent','snapshot',n->'snapshot') THEN RETURN false;END IF;
 e:=m->'entries'->ordinal;
 namespace:=photo.semantic_uuid_v5(parent,'semantic.target.prepare:1:'||(n->'snapshot'->>'sha256')||':'||ordinal||':'||
 encode(public.digest(convert_to(photo.v27_target_manifest_json_text(e->'binding'),'UTF8'),'sha256'),'hex'));
 IF e->'ordinal' IS DISTINCT FROM to_jsonb(ordinal) OR e->'reuse_reference' IS DISTINCT FROM 'false'::jsonb
 OR e->'binding' IS NOT DISTINCT FROM n->'source' OR e->'binding' IS DISTINCT FROM c->'source'
 OR e->>'namespace' IS DISTINCT FROM namespace::text OR c->>'target_namespace' IS DISTINCT FROM namespace::text
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'semantic.face.detect:reference')
 OR c->>'target_stage_id' IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'stage:face.detect')::text
 OR e->'stages'->1 IS DISTINCT FROM jsonb_build_object('id',photo.semantic_uuid_v5(namespace,'stage:face.detect'),'operation','face.detect','operation_version','1','requires','[]'::jsonb)
 OR c->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'semantic.face.detect.params:reference')::text
 OR c->>'task_id' IS DISTINCT FROM p_task::text OR c->>'input_digest' IS DISTINCT FROM t.input_digest
 OR c->'params'->>'artifact_id' IS DISTINCT FROM t.params_artifact_id::text
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cuda'
 OR coalesce(t.execution_profile->>'model_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(t.execution_profile->>'runtime_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(t.execution_profile->>'device_profile_sha','') !~ '^[0-9a-f]{64}$'
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
 AND w.id=photo.semantic_uuid_v5(p_run,'workflow') AND w.project_id=project AND w.input_snapshot_id=r.input_snapshot_id
 AND w.workflow_type='semantic' AND w.renderer_version='v2.2' AND w.policy_sha=c->>'policy_sha') THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v27_target_detector_graph_scope(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v27_target_detector_graph_scope(uuid,uuid,uuid) FROM PUBLIC;
CREATE FUNCTION photo.semantic_target_detector_registration_state(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_params_sha text,p_digest text,p_profile jsonb,p_rows jsonb,p_manifest jsonb,p_chunks jsonb,p_direct jsonb,p_count integer,p_entries_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE; actual jsonb; transport jsonb; expected_transport jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'conflict';END IF;
 IF photo.lock_v27_target_detector_graph_scope(p_tenant,p_run,p_task) IS NOT TRUE THEN RETURN 'conflict';END IF;
 IF p_count IS NULL OR p_count NOT BETWEEN 3 AND 4097 OR p_entries_sha IS NULL OR p_entries_sha !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array' OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_chunks)<>(p_count+511)/512 OR jsonb_typeof(p_manifest) IS DISTINCT FROM 'object'
 OR encode(public.digest(convert_to(photo.v27_target_manifest_json_text(p_rows),'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_entries_sha THEN RETURN 'conflict';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR t.state<>'blocked' OR t.cancel_requested OR t.attempt<>0 OR t.operation<>'face.detect' OR t.resource_class<>'gpu' OR t.operation_version<>'1'
 OR jsonb_typeof(r.config_snapshot->'v27_target_detector_prepare') IS DISTINCT FROM 'object'
 OR r.config_snapshot->'v27_target_detector_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((r.config_snapshot->'v27_target_detector_prepare'->>'target_namespace')::uuid,'semantic.face.detect:reference')
 OR r.config_snapshot->'v27_target_detector_prepare'->>'task_id' IS DISTINCT FROM p_task::text
 OR r.config_snapshot->'v27_target_detector_prepare'->>'input_digest' IS DISTINCT FROM p_digest
 OR r.config_snapshot->'v27_target_detector_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)
 OR jsonb_typeof(p_direct) IS DISTINCT FROM 'array' OR jsonb_array_length(p_direct)<>3
 OR NOT (p_direct @> jsonb_build_array(jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)))
 OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_params_sha AND state='ready')
 OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array' OR jsonb_array_length(p_rows)<>p_count
 OR p_count NOT BETWEEN 3 AND 4097 OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array' THEN RETURN 'conflict';END IF;
 IF p_direct->0 IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)
 OR p_direct->1 IS DISTINCT FROM r.config_snapshot->'v27_target_detector_prepare'->'source'->'source'
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_rows) AS pins(value) WHERE pins.value->'ref'=p_direct->2 AND pins.value->>'contract'='NpyArray') THEN RETURN 'conflict';END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal),'[]') INTO actual
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal<1000000;
 IF NOT (t.execution_profile ? 'v2_input_manifest') THEN
  IF actual='[]' AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task)
   AND t.execution_profile=p_profile AND t.inputs='[]'::jsonb THEN RETURN 'missing';END IF;
  RETURN 'conflict';
 END IF;
 IF actual IS DISTINCT FROM p_rows OR t.inputs IS DISTINCT FROM jsonb_build_array(p_manifest)||p_direct
 OR t.execution_profile->'v2_input_manifest' IS DISTINCT FROM p_manifest
 OR t.execution_profile->>'v2_entry_count' IS DISTINCT FROM p_count::text
 OR t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM p_entries_sha
 OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM p_profile
 THEN RETURN 'conflict';END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) ORDER BY i.ordinal),'[]') INTO transport
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=1000000;
 expected_transport:=p_chunks||jsonb_build_array(p_manifest);
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=1000000
 AND (i.ordinal NOT BETWEEN 1000000 AND 1000000+jsonb_array_length(p_chunks)
 OR i.contract IS DISTINCT FROM CASE WHEN i.ordinal=1000000+jsonb_array_length(p_chunks) THEN 'InputClosureManifest' ELSE 'InputClosureChunk' END)) THEN RETURN 'conflict';END IF;
 IF transport IS DISTINCT FROM expected_transport OR NOT photo.v2_input_closure_ready(p_task) THEN RETURN 'conflict';END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(expected_transport) WITH ORDINALITY ref(value,n)
 WHERE NOT EXISTS(SELECT 1 FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_task
 AND b.state='ready' AND b.artifact_id=(ref.value->>'artifact_id')::uuid AND b.sha256=ref.value->>'sha256'
 AND b.purpose=CASE WHEN ref.n=jsonb_array_length(expected_transport) THEN 'v2_input_manifest' ELSE 'v2_input_chunk_'||(ref.n-1)::text END)) THEN RETURN 'conflict';END IF;
 RETURN 'replayed';
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN 'conflict';
END $$;

ALTER FUNCTION photo.semantic_target_detector_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_detector_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_detector_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_pending_shell_matches(p_tenant uuid,p_run uuid,p_workflow uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_type text,p_policy_sha text,p_config_sha text,p_config jsonb,p_tasks jsonb,p_edges jsonb,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; profile jsonb; amended jsonb:='[]';task uuid;
BEGIN
 IF p_type='semantic' AND p_config ? 'v27_target_detector_prepare' THEN
 IF jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' OR jsonb_array_length(p_tasks)<>1
 OR jsonb_typeof(p_config->'v27_target_detector_prepare') IS DISTINCT FROM 'object'
 OR p_config->'v27_target_detector_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_config ? 'v27_person_prepare' OR p_config ? 'v27_face_prepare' OR p_config ? 'v27_skin_prepare'
 OR p_config ? 'v27_defect_prepare' OR p_config ? 'v27_detector_prepare'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((p_config->'v27_target_detector_prepare'->>'target_namespace')::uuid,'semantic.face.detect:reference')
 OR p_config ? 'v27_target_person_prepare' OR jsonb_array_length(p_edges)<>0 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
  task:=(item->>'id')::uuid;
  SELECT execution_profile INTO profile FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task;
  IF profile ? 'v2_input_manifest' THEN
   IF item->>'operation' IS DISTINCT FROM 'face.detect'
   OR item->>'operation_version' IS DISTINCT FROM '1' OR item->>'resource_class' IS DISTINCT FROM 'gpu'
   OR p_config->'v27_target_detector_prepare'->>'task_id' IS DISTINCT FROM task::text
   OR p_config->'v27_target_detector_prepare'->>'input_digest' IS DISTINCT FROM item->>'input_digest'
   OR p_config->'v27_target_detector_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',item->>'params_artifact_id','sha256',item->>'params_sha')
   OR (item->'execution_profile') IS DISTINCT FROM (profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
   OR NOT photo.v2_input_closure_ready(task) THEN RETURN false;END IF;
   item:=jsonb_set(item,'{execution_profile}',profile);
  END IF;
  amended:=amended||jsonb_build_array(item);
 END LOOP;
 RETURN photo.v2_pending_shell_matches_pre_v27_registered(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,amended,p_edges,p_bytes);
 END IF;
 IF p_type='semantic' AND p_config ? 'v27_target_person_prepare' THEN
 IF jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' OR jsonb_array_length(p_tasks)<>1
 OR jsonb_typeof(p_config->'v27_target_person_prepare') IS DISTINCT FROM 'object'
 OR p_config->'v27_target_person_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_config ? 'v27_person_prepare' OR p_config ? 'v27_face_prepare' OR p_config ? 'v27_skin_prepare'
 OR p_config ? 'v27_defect_prepare' OR p_config ? 'v27_detector_prepare'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((p_config->'v27_target_person_prepare'->>'target_namespace')::uuid,'semantic.mask.person:reference')
 OR jsonb_array_length(p_edges)<>0 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
  task:=(item->>'id')::uuid;
  SELECT execution_profile INTO profile FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task;
  IF profile ? 'v2_input_manifest' THEN
   IF item->>'operation' IS DISTINCT FROM 'mask.person'
   OR item->>'operation_version' IS DISTINCT FROM '1' OR item->>'resource_class' IS DISTINCT FROM 'gpu'
   OR p_config->'v27_target_person_prepare'->>'task_id' IS DISTINCT FROM task::text
   OR p_config->'v27_target_person_prepare'->>'input_digest' IS DISTINCT FROM item->>'input_digest'
   OR p_config->'v27_target_person_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',item->>'params_artifact_id','sha256',item->>'params_sha')
   OR (item->'execution_profile') IS DISTINCT FROM (profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
   OR NOT photo.v2_input_closure_ready(task) THEN RETURN false;END IF;
   item:=jsonb_set(item,'{execution_profile}',profile);
  END IF;
  amended:=amended||jsonb_build_array(item);
 END LOOP;
 RETURN photo.v2_pending_shell_matches_pre_v27_registered(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,amended,p_edges,p_bytes);
 END IF;
 IF p_type IS DISTINCT FROM 'semantic' OR NOT (p_config ? 'v27_skin_prepare') THEN
  RETURN photo.v2_pending_shell_matches_pre_v27_skin(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,p_tasks,p_edges,p_bytes);
 END IF;
 IF jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' OR jsonb_array_length(p_tasks)<>1
 OR jsonb_typeof(p_config->'v27_skin_prepare') IS DISTINCT FROM 'object'
 OR p_config->'v27_skin_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR coalesce(p_config->'v27_skin_prepare'->>'operation','') NOT IN ('mask.face_skin')
 OR p_config ? 'v27_face_prepare' OR p_config ? 'v27_person_prepare'
 OR p_config ? 'v27_defect_prepare' OR p_config ? 'v27_detector_prepare'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((p_config->'v27_skin_prepare'->>'parent_run_id')::uuid,'semantic.'||(p_config->'v27_skin_prepare'->>'operation')||':reference')
 OR jsonb_array_length(p_edges)<>0 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
  task:=(item->>'id')::uuid;
  SELECT execution_profile INTO profile FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task;
  IF profile ? 'v2_input_manifest' THEN
   IF item->>'operation' IS DISTINCT FROM p_config->'v27_skin_prepare'->>'operation'
   OR item->>'operation_version' IS DISTINCT FROM '1' OR item->>'resource_class' IS DISTINCT FROM 'cpu'
   OR p_config->'v27_skin_prepare'->>'task_id' IS DISTINCT FROM task::text
   OR p_config->'v27_skin_prepare'->>'input_digest' IS DISTINCT FROM item->>'input_digest'
   OR p_config->'v27_skin_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',item->>'params_artifact_id','sha256',item->>'params_sha')
   OR (item->'execution_profile') IS DISTINCT FROM (profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
   OR NOT photo.v2_input_closure_ready(task) THEN RETURN false;END IF;
   item:=jsonb_set(item,'{execution_profile}',profile);
  END IF;
  amended:=amended||jsonb_build_array(item);
 END LOOP;
 RETURN photo.v2_pending_shell_matches_pre_v27_registered(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,amended,p_edges,p_bytes);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
