-- +goose Up
-- Dedicated target face activation and current-authority sealed replay.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v27_target_face_sealed_graph_scope(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;d photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c jsonb;n jsonb;m jsonb;e jsonb;
 parent uuid;detector uuid;project uuid;session_id uuid;namespace uuid;ordinal integer;op text;td photo.runs%ROWTYPE;dt photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT FOUND OR jsonb_typeof(r.config_snapshot->'v27_target_face_prepare') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_target_face_prepare';op:=c->>'operation';IF op IS NULL OR op NOT IN('face.landmarks','face.parse') THEN RETURN false;END IF;parent:=(c->>'parent_run_id')::uuid;
 detector:=photo.semantic_uuid_v5(parent,'semantic.face.detect:reference');
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=parent FOR SHARE;
 SELECT * INTO d FROM photo.runs WHERE tenant_id=p_tenant AND id=detector FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF d.id IS NULL OR r.id IS NULL OR t.id IS NULL OR d.kind<>'v2' OR NOT d.graph_sealed OR d.cancel_requested
 OR d.status NOT IN('queued','running','succeeded') OR d.v27_initial_manual_selection IS NOT NULL
 OR r.kind<>'v2' OR r.status NOT IN('queued','running','succeeded') OR NOT r.graph_sealed OR r.cancel_requested
 OR t.state NOT IN('ready','queued','running','retry_wait','succeeded') OR t.cancel_requested
 OR t.operation IS DISTINCT FROM op OR t.operation_version<>'1' OR t.resource_class<>'gpu' OR t.node_key IS DISTINCT FROM op||':0'
 OR p_task IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(p_run,'workflow'),op||':0') THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_target_face_prepare';n:=d.config_snapshot->'v27_detector_prepare';
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
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'semantic.'||op||':reference')
 OR c->>'target_stage_id' IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'stage:'||op)::text
 OR e->'stages'->(CASE WHEN op='face.landmarks' THEN 2 ELSE 3 END) IS DISTINCT FROM jsonb_build_object('id',photo.semantic_uuid_v5(namespace,'stage:'||op),'operation',op,'operation_version','1','requires',jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:face.detect')))
 OR c->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'semantic.'||op||'.params:reference')::text
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
 SELECT * INTO td FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5(namespace,'semantic.face.detect:reference') FOR SHARE;
 SELECT * INTO dt FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=td.id AND id=photo.semantic_uuid_v5(photo.semantic_uuid_v5(td.id,'workflow'),'face.detect:0') FOR SHARE;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=dt.id AND id=dt.canonical_result_id AND attempt=dt.attempt AND fencing_token=dt.fencing_token FOR SHARE;
 IF td.id IS NULL OR dt.id IS NULL OR result.id IS NULL OR td.status<>'completed' OR NOT td.graph_sealed OR td.cancel_requested
 OR td.created_by IS DISTINCT FROM r.created_by OR td.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR td.config_snapshot->'v27_target_detector_prepare'->'source' IS DISTINCT FROM c->'source'
 OR td.config_snapshot->'v27_target_detector_prepare'->'manifest_sha' IS DISTINCT FROM c->'manifest_sha'
 OR td.config_snapshot->'v27_target_detector_prepare'->'auth_session_id' IS DISTINCT FROM c->'auth_session_id'
 OR c->>'detector_run_id' IS DISTINCT FROM td.id::text OR c->>'detector_task_id' IS DISTINCT FROM dt.id::text
 OR c->>'detector_result_id' IS DISTINCT FROM result.id::text OR c->'detector_output' IS DISTINCT FROM result.outputs->0
 OR c->'detector_params' IS DISTINCT FROM td.config_snapshot->'v27_target_detector_prepare'->'params'
 OR photo.semantic_target_detector_completed_output_ready(p_tenant,dt.id,dt.attempt,dt.fencing_token,result.outputs) IS NOT TRUE THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v27_target_face_sealed_graph_scope(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v27_target_face_sealed_graph_scope(uuid,uuid,uuid) FROM PUBLIC;
CREATE FUNCTION photo.semantic_target_face_sealed_registration_state(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_params_sha text,p_digest text,p_profile jsonb,p_rows jsonb,p_manifest jsonb,p_chunks jsonb,p_direct jsonb,p_count integer,p_entries_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE; actual jsonb; transport jsonb; expected_transport jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'conflict';END IF;
 IF photo.lock_v27_target_face_sealed_graph_scope(p_tenant,p_run,p_task) IS NOT TRUE THEN RETURN 'conflict';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('queued','running','succeeded') OR NOT r.graph_sealed OR r.cancel_requested
 OR t.state NOT IN('ready','queued','running','retry_wait','succeeded') OR t.cancel_requested OR t.operation NOT IN ('face.landmarks','face.parse') OR t.resource_class<>'gpu' OR t.operation_version<>'1'
 OR jsonb_typeof(r.config_snapshot->'v27_target_face_prepare') IS DISTINCT FROM 'object'
 OR r.config_snapshot->'v27_target_face_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR r.config_snapshot->'v27_target_face_prepare'->>'operation' IS DISTINCT FROM t.operation
 OR p_profile->>'backend' IS DISTINCT FROM 'cuda'
 OR r.config_snapshot ? 'v27_detector_prepare' OR r.config_snapshot ? 'v27_person_prepare' OR r.config_snapshot ? 'v27_defect_prepare'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((r.config_snapshot->'v27_target_face_prepare'->>'target_namespace')::uuid,'semantic.'||t.operation||':reference')
 OR r.config_snapshot->'v27_target_face_prepare'->>'task_id' IS DISTINCT FROM p_task::text
 OR r.config_snapshot->'v27_target_face_prepare'->>'input_digest' IS DISTINCT FROM p_digest
 OR r.config_snapshot->'v27_target_face_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)
 OR jsonb_typeof(p_direct) IS DISTINCT FROM 'array' OR jsonb_array_length(p_direct)<>3
 OR NOT (p_direct @> jsonb_build_array(jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)))
 OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_params_sha AND state='ready')
 OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array' OR jsonb_array_length(p_rows)<>p_count
 OR p_count NOT BETWEEN 4 AND 4097 OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array' THEN RETURN 'conflict';END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(p_rows) WHERE value->>'contract'='Params')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_rows) WHERE value->>'contract'='WorkingImage')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_rows) WHERE value->>'contract'='FaceBoxes')<>1
 OR NOT p_rows @> jsonb_build_array(jsonb_build_object('contract','Params','ref',jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)))
 OR NOT p_rows @> jsonb_build_array(jsonb_build_object('contract','WorkingImage','ref',r.config_snapshot->'v27_target_face_prepare'->'source'->'source'))
 OR NOT p_rows @> jsonb_build_array(jsonb_build_object('contract','FaceBoxes','ref',r.config_snapshot->'v27_target_face_prepare'->'detector_output'))
 THEN RETURN 'conflict';END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal),'[]') INTO actual
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal<1000000;
 IF NOT (t.execution_profile ? 'v2_input_manifest') THEN RETURN 'conflict';END IF;
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
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_target_face_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_face_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) FROM PUBLIC;

-- Strict native FAN/BiSeNet registration envelope. No activation or model authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_target_face_sealed_registration_binding_state(p_tenant uuid,p_run uuid,p_operation text,p_binding jsonb)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE transport jsonb;params jsonb;count_entries integer;ref jsonb;row_entry jsonb;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN ('face.landmarks','face.parse') THEN RETURN 'conflict';END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_binding) IS DISTINCT FROM 'object' OR octet_length(p_binding::text)>4194304
 OR p_binding->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_binding-'schema_version'-'task_id'-'params'-'input_digest'-'profile'-'transport')<>'{}'
 OR jsonb_typeof(p_binding->'task_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_binding->'input_digest') IS DISTINCT FROM 'string'
 OR coalesce(p_binding->>'input_digest','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_binding->'profile') IS DISTINCT FROM 'object' THEN RETURN 'conflict';END IF;
 params:=p_binding->'params';transport:=p_binding->'transport';
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR (params-'artifact_id'-'sha256')<>'{}'
 OR jsonb_typeof(params->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'sha256') IS DISTINCT FROM 'string'
 OR coalesce(params->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(transport) IS DISTINCT FROM 'object'
 OR transport->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (transport-'schema_version'-'rows'-'manifest'-'chunks'-'direct'-'count'-'entries_sha')<>'{}'
 OR jsonb_typeof(transport->'count') IS DISTINCT FROM 'number'
 OR coalesce(transport->>'count','') !~ '^[0-9]+$'
 OR jsonb_typeof(transport->'entries_sha') IS DISTINCT FROM 'string'
 OR coalesce(transport->>'entries_sha','') !~ '^[0-9a-f]{64}$'
 OR (p_binding->>'task_id')::uuid='00000000-0000-0000-0000-000000000000'
 OR (params->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000' THEN RETURN 'conflict';END IF;
 count_entries:=(transport->>'count')::integer;
 IF encode(public.digest(convert_to(photo.v27_target_manifest_json_text(transport->'rows'),'UTF8'),'sha256'),'hex') IS DISTINCT FROM transport->>'entries_sha' THEN RETURN 'conflict';END IF;
 IF count_entries NOT BETWEEN 4 AND 4097 THEN RETURN 'conflict';END IF;
 IF jsonb_typeof(transport->'rows') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'rows')<>count_entries
 OR jsonb_typeof(transport->'chunks') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'chunks')<>(count_entries+511)/512
 OR jsonb_typeof(transport->'direct') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'direct')<>3
 OR jsonb_typeof(transport->'manifest') IS DISTINCT FROM 'object' THEN RETURN 'conflict';END IF;
 FOR row_entry IN SELECT value FROM jsonb_array_elements(transport->'rows') LOOP
  IF jsonb_typeof(row_entry) IS DISTINCT FROM 'object' OR (row_entry-ARRAY['ref','contract'])<>'{}'
  OR row_entry->>'contract' IS NULL OR row_entry->>'contract' NOT IN ('Params','WorkingImage','NpyArray','FaceBoxes') THEN RETURN 'conflict';END IF;
 END LOOP;
 FOR ref IN SELECT row_entry.value->'ref' FROM jsonb_array_elements(transport->'rows') row_entry
 UNION ALL SELECT value FROM jsonb_array_elements(transport->'chunks')
 UNION ALL SELECT value FROM jsonb_array_elements(transport->'direct')
 UNION ALL SELECT transport->'manifest' LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-ARRAY['artifact_id','sha256'])<>'{}'
  OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR (ref->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000' THEN RETURN 'conflict';END IF;
 END LOOP;
 IF (SELECT count(DISTINCT value->'ref'->>'artifact_id') FROM jsonb_array_elements(transport->'rows'))<>count_entries
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(transport->'direct'))<>3 THEN RETURN 'conflict';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_binding->>'task_id')::uuid AND operation=p_operation) THEN RETURN 'conflict';END IF;
 RETURN photo.semantic_target_face_sealed_registration_state(p_tenant,p_run,(p_binding->>'task_id')::uuid,
  (params->>'artifact_id')::uuid,params->>'sha256',p_binding->>'input_digest',p_binding->'profile',
  transport->'rows',transport->'manifest',transport->'chunks',transport->'direct',count_entries,transport->>'entries_sha');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN 'conflict';
END $$;
ALTER FUNCTION photo.semantic_target_face_sealed_registration_binding_state(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_face_sealed_registration_binding_state(uuid,uuid,text,jsonb) FROM PUBLIC;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_target_face_sealed_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE face jsonb;detector jsonb;result jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 config jsonb;native jsonb;parent_native jsonb;operation text;project uuid;actor uuid;session_id uuid;
 original jsonb;detector_packet jsonb;pin jsonb;key_name text;files jsonb;rows jsonb;registration jsonb;output_pin jsonb;qualified boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','face','detector','detector_result'])<>'{}' THEN RETURN false;END IF;
 face:=p_packet->'face';detector_packet:=p_packet->'detector';detector:=detector_packet->'target';original:=detector_packet->'original';result:=p_packet->'detector_result';
 IF jsonb_typeof(face) IS DISTINCT FROM 'object' OR face->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR face->>'tenant_id' IS DISTINCT FROM p_tenant::text OR face->>'run_id' IS DISTINCT FROM p_run::text
 OR (face-ARRAY['schema_version','operation','tenant_id','project_id','actor_id','session_id','run_id','parent_run_id','request_sha','config_text','policy_text','anchor','binding','icc_sha','unit_rgb','source_pins','expected','qualification','params_document_id','params_pin','files','registration'])<>'{}'
 OR jsonb_typeof(face->'config_text') IS DISTINCT FROM 'string' OR octet_length(face->>'config_text') NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(face->'policy_text') IS DISTINCT FROM 'string' OR octet_length(face->>'policy_text') NOT BETWEEN 2 AND 4194304
 OR coalesce(face->>'operation','') NOT IN ('face.landmarks','face.parse')
 OR detector->>'operation' IS DISTINCT FROM 'face.detect' THEN RETURN false;END IF;
 FOREACH key_name IN ARRAY ARRAY['tenant_id','project_id','actor_id','session_id','parent_run_id','request_sha','policy_text','anchor','binding','icc_sha','unit_rgb','source_pins'] LOOP
  IF face->key_name IS DISTINCT FROM detector->key_name THEN RETURN false;END IF;
 END LOOP;
 operation:=face->>'operation';project:=(face->>'project_id')::uuid;actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;
 IF photo.lock_semantic_target_detector_result(p_tenant,(detector->>'run_id')::uuid,detector_packet,(result->>'result_id')::uuid,jsonb_build_array(result->'output')) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status NOT IN('queued','running','succeeded') OR NOT r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 config:=(face->>'config_text')::jsonb;native:=config->'v27_target_face_prepare';parent_native:=(detector->>'config_text')::jsonb->'v27_target_detector_prepare';
 IF config IS DISTINCT FROM r.config_snapshot OR (config-ARRAY['project_id','v27_target_face_prepare'])<>'{}'
 OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(face->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(face->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['target_ordinal','target_namespace','target_stage_id','manifest_sha','recovery','schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','detector_run_id','detector_task_id','detector_result_id','detector_output','detector_params'])<>'{}'
 OR native->>'operation' IS DISTINCT FROM operation
 OR native->'parent_run_id' IS DISTINCT FROM face->'parent_run_id' OR native->'request_sha' IS DISTINCT FROM face->'request_sha'
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text OR native->>'policy_sha' IS DISTINCT FROM w.policy_sha
 OR native->'source' IS DISTINCT FROM face->'binding' OR native->'intent' IS DISTINCT FROM parent_native->'intent' OR native->'snapshot' IS DISTINCT FROM parent_native->'snapshot'
 OR native->'params_document' IS DISTINCT FROM face->'params_document_id'
 OR native->'params' IS DISTINCT FROM face->'registration'->'params' OR native->'task_id' IS DISTINCT FROM face->'registration'->'task_id'
 OR native->'input_digest' IS DISTINCT FROM face->'registration'->'input_digest'
 OR native->'detector_run_id' IS DISTINCT FROM detector->'run_id'
 OR native->'detector_task_id' IS DISTINCT FROM detector->'registration'->'task_id'
 OR native->'detector_result_id' IS DISTINCT FROM result->'result_id'
 OR native->'detector_output' IS DISTINCT FROM result->'output' OR native->'detector_params' IS DISTINCT FROM result->'params'
 OR result->'params' IS DISTINCT FROM detector->'registration'->'params' THEN RETURN false;END IF;
 IF native->'target_namespace' IS DISTINCT FROM parent_native->'target_namespace'
 OR native->'target_ordinal' IS DISTINCT FROM parent_native->'target_ordinal'
 OR native->'manifest_sha' IS DISTINCT FROM parent_native->'manifest_sha'
 OR native->'source' IS DISTINCT FROM parent_native->'source'
 OR photo.lock_v27_target_preparation_entry(p_tenant,(original->>'run_id')::uuid,original,native->>'manifest_sha',(native->>'target_ordinal')::integer,operation) IS NULL THEN RETURN false;END IF;
 IF jsonb_typeof(result) IS DISTINCT FROM 'object' OR result->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (result-ARRAY['schema_version','result_id','output','params','files'])<>'{}'
 OR jsonb_typeof(result->'files') IS DISTINCT FROM 'array' OR jsonb_array_length(result->'files')<>2 THEN RETURN false;END IF;
 FOR pin IN SELECT value FROM jsonb_array_elements(result->'files') LOOP
  IF pin->>'role' NOT IN('detector_params','face_boxes') OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(pin->>'artifact_id')::uuid AND a.sha256=pin->>'sha256' AND a.bucket=pin->>'bucket'
  AND a.object_key=pin->>'key' AND a.object_version=pin->>'version' AND a.bytes=(pin->>'bytes')::bigint
  AND a.media_type=pin->>'media_type' AND a.state='ready' AND a.verified_at IS NOT NULL
  AND jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)=CASE WHEN pin->>'role'='face_boxes' THEN result->'output' ELSE result->'params' END) THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT count(DISTINCT value->>'role') FROM jsonb_array_elements(result->'files'))<>2 THEN RETURN false;END IF;
 IF face->'expected'->>'Operation' IS DISTINCT FROM operation OR face->'expected'->>'Version' IS DISTINCT FROM '1'
 OR face->'expected'->>'Backend' IS DISTINCT FROM 'cuda' THEN RETURN false;END IF;
 IF operation='face.landmarks' THEN qualified:=photo.lock_semantic_proposal_face_qualification(p_tenant,project,actor,session_id,face->'expected',face->'qualification');
 ELSE qualified:=photo.lock_semantic_proposal_face_parse_qualification(p_tenant,project,actor,session_id,face->'expected',face->'qualification');END IF;
 IF qualified IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_version_params(p_tenant,project,actor,session_id,(face->'binding'->>'asset_version_id')::uuid,(face->'binding'->'source'->>'artifact_id')::uuid,face->'binding'->'source'->>'sha256',(face->>'params_document_id')::uuid,(native->'params'->>'artifact_id')::uuid,native->'params'->>'sha256',face->'params_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF jsonb_typeof(face->'files') IS DISTINCT FROM 'array' OR jsonb_array_length(face->'files') NOT BETWEEN 4 AND 4097
 OR (SELECT count(*) FROM jsonb_array_elements(face->'files') WHERE value->>'Contract'='FaceBoxes')<>1 THEN RETURN false;END IF;
 SELECT value INTO output_pin FROM jsonb_array_elements(result->'files') WHERE value->>'role'='face_boxes';
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(face->'files') file WHERE (file-ARRAY['Ref','Contract','Object'])='{}' AND file->>'Contract'='FaceBoxes'
 AND file->'Ref'=result->'output'
 AND file->'Object'=jsonb_build_object('Key',output_pin->'key','VersionID',output_pin->'version','SHA256',output_pin->'sha256','Bytes',output_pin->'bytes','ContentType',output_pin->'media_type')) THEN RETURN false;END IF;
 SELECT jsonb_agg(value ORDER BY n) INTO files FROM jsonb_array_elements(face->'files') WITH ORDINALITY item(value,n) WHERE value->>'Contract'<>'FaceBoxes';
 SELECT jsonb_agg(value ORDER BY n) INTO rows FROM jsonb_array_elements(face->'registration'->'transport'->'rows') WITH ORDINALITY item(value,n) WHERE value->>'contract'<>'FaceBoxes';
 registration:=jsonb_set(face->'registration','{transport,rows}',rows);
 IF photo.semantic_native_file_closure_matches(face->'binding',face->'source_pins',native->'params',face->'params_pin',files,registration) IS NOT TRUE
 OR photo.semantic_target_face_sealed_registration_binding_state(p_tenant,p_run,operation,face->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run)
 AND NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links WHERE tenant_id=p_tenant AND workflow_id=w.id);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_face_sealed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_face_sealed_evidence(uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_target_face_preparation(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_evidence jsonb;entry jsonb;native jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;i photo.idempotency_records%ROWTYPE;t photo.tasks%ROWTYPE;actor uuid;session_id uuid;project uuid;ticket jsonb;next_cursor bigint;event_id uuid;admission_key text;changed integer;
BEGIN
 IF jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb OR (p_packet-ARRAY['schema_version','face','detector','detector_result'])<>'{}' THEN RETURN NULL;END IF;
 p_evidence:=p_packet->'face';
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432 THEN RETURN NULL;END IF;
 actor:=(p_evidence->>'actor_id')::uuid;session_id:=(p_evidence->>'session_id')::uuid;project:=(p_evidence->>'project_id')::uuid;IF p_evidence->>'operation' IS NULL OR p_evidence->>'operation' NOT IN('face.landmarks','face.parse') THEN RETURN NULL;END IF;
 admission_key:='target-face-prep:'||p_run::text;
 native:=(p_evidence->>'config_text')::jsonb->'v27_target_face_prepare';
 entry:=photo.lock_v27_target_preparation_entry(p_tenant,(p_packet->'detector'->'original'->>'run_id')::uuid,p_packet->'detector'->'original',native->>'manifest_sha',(native->>'target_ordinal')::integer,p_evidence->>'operation');
 IF entry IS NULL THEN RETURN NULL;END IF;
 IF photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS NOT TRUE THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM actor OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic'
 OR p_evidence->>'tenant_id' IS DISTINCT FROM p_tenant::text OR p_evidence->>'run_id' IS DISTINCT FROM p_run::text
 OR (p_evidence->>'config_text')::jsonb IS DISTINCT FROM r.config_snapshot
 OR encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(p_evidence->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha THEN RETURN NULL;END IF;
 SELECT * INTO i FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=actor AND route='V2.Admit' AND idempotency_records.key=admission_key FOR UPDATE;
 IF NOT FOUND OR i.resource_id IS DISTINCT FROM p_run OR i.request_hash IS DISTINCT FROM p_evidence->>'request_sha' THEN RETURN NULL;END IF;
 ticket:=jsonb_build_object('run_id',p_run,'workflow_id',w.id,'state','queued');
 IF r.graph_sealed THEN
  IF i.response_status=202 AND i.response_body=ticket AND photo.lock_semantic_target_face_sealed_evidence(p_tenant,p_run,p_packet) IS TRUE THEN RETURN ticket;END IF;
  RETURN NULL;
 END IF;
 IF i.response_status IS NOT NULL OR i.expires_at<=clock_timestamp() THEN RETURN NULL;END IF;
 -- Native preparation currently has no consent-bearing admission contract.
 -- Reject links rather than silently bypassing generic consent epoch guards.
 IF EXISTS(SELECT 1 FROM photo.workflow_consent_links WHERE tenant_id=p_tenant AND workflow_id=w.id) THEN RETURN NULL;END IF;
 IF photo.lock_semantic_target_face_admission_evidence(p_tenant,p_run,p_packet) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_evidence->'registration'->>'task_id')::uuid FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version AND v2_enabled AND resource_class=t.resource_class AND backends ? 'cuda') THEN RETURN NULL;END IF;
 IF photo.lock_semantic_face_graph_budget(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN NULL;END IF;
 UPDATE photo.runs SET graph_sealed=true,status='queued',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run AND status='draft' AND NOT graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'V27_NATIVE_SEAL_RACE' USING ERRCODE='40001';END IF;
 IF photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) IS NOT TRUE THEN RAISE EXCEPTION 'V27_NATIVE_ACTIVATION_FAILED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(p_tenant,next_cursor,p_run,actor,'principal','v2.run.queued',jsonb_build_object('run_id',p_run,'status','queued','revision',r.revision+1));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(p_tenant,event_id,'photo.events.v1','v2.run.queued',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.queued','tenant_id',p_tenant,'run_id',p_run,'status','queued'));
 UPDATE photo.idempotency_records SET response_status=202,response_body=ticket WHERE tenant_id=p_tenant AND id=i.id AND response_status IS NULL;
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>1 THEN RAISE EXCEPTION 'V27_NATIVE_TICKET_RACE' USING ERRCODE='40001';END IF;
 RETURN ticket;
END $$;
ALTER FUNCTION photo.seal_semantic_target_face_preparation(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_target_face_preparation(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_semantic_target_face_preparation(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
