-- +goose Up
-- Atomic target defect seal with current-authority sealed replay.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v27_target_defect_sealed_graph_scope(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;d photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c jsonb;n jsonb;m jsonb;e jsonb;
 parent uuid;detector uuid;project uuid;session_id uuid;namespace uuid;ordinal integer;op text;td photo.runs%ROWTYPE;dt photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT FOUND OR jsonb_typeof(r.config_snapshot->'v27_target_defect_prepare') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_target_defect_prepare';op:=c->>'operation';IF op IS NULL OR op<>'defects.propose' THEN RETURN false;END IF;parent:=(c->>'parent_run_id')::uuid;
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
 OR t.operation IS DISTINCT FROM op OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.node_key IS DISTINCT FROM op||':0'
 OR p_task IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(p_run,'workflow'),op||':0') THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_target_defect_prepare';n:=d.config_snapshot->'v27_detector_prepare';
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
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'semantic.defect.prepare:reference')
 OR c->>'target_stage_id' IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'stage:'||op)::text
 OR e->'stages'->5 IS DISTINCT FROM jsonb_build_object('id',photo.semantic_uuid_v5(namespace,'stage:'||op),'operation',op,'operation_version','1','requires',jsonb_build_array(photo.semantic_uuid_v5(namespace,'stage:mask.person'),photo.semantic_uuid_v5(namespace,'stage:face.landmarks'),photo.semantic_uuid_v5(namespace,'stage:mask.face_skin')))
 OR c->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'semantic.defect.params:reference')::text
 OR c->>'task_id' IS DISTINCT FROM p_task::text OR c->>'input_digest' IS DISTINCT FROM t.input_digest
 OR c->'params'->>'artifact_id' IS DISTINCT FROM t.params_artifact_id::text
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
 AND w.id=photo.semantic_uuid_v5(p_run,'workflow') AND w.project_id=project AND w.input_snapshot_id=r.input_snapshot_id
 AND w.workflow_type='semantic' AND w.renderer_version='v2.2' AND w.policy_sha=c->>'policy_sha') THEN RETURN false;END IF;
 RETURN photo.lock_semantic_target_defect_completed_dependencies(p_tenant,project,r.created_by,session_id,namespace,jsonb_build_array(
 jsonb_build_object('operation','mask.person','run_id',c->'person_run_id','task_id',c->'person_task_id','result_id',c->'person_result_id'),
 jsonb_build_object('operation','face.landmarks','run_id',c->'landmarks_run_id','task_id',c->'landmarks_task_id','result_id',c->'landmarks_result_id'),
 jsonb_build_object('operation','mask.face_skin','run_id',c->'skin_run_id','task_id',c->'skin_task_id','result_id',c->'skin_result_id')));
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v27_target_defect_sealed_graph_scope(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v27_target_defect_sealed_graph_scope(uuid,uuid,uuid) FROM PUBLIC;
CREATE FUNCTION photo.semantic_target_defect_sealed_registration_state(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_params_sha text,p_digest text,p_profile jsonb,p_rows jsonb,p_manifest jsonb,p_chunks jsonb,p_direct jsonb,p_count integer,p_entries_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE; actual jsonb; transport jsonb; expected_transport jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'conflict';END IF;
 IF photo.lock_v27_target_defect_sealed_graph_scope(p_tenant,p_run,p_task) IS NOT TRUE THEN RETURN 'conflict';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('queued','running','succeeded') OR NOT r.graph_sealed OR r.cancel_requested
 OR t.state NOT IN('ready','queued','running','retry_wait','succeeded') OR t.cancel_requested OR t.operation<>'defects.propose' OR t.operation_version<>'1'
 OR jsonb_typeof(r.config_snapshot->'v27_target_defect_prepare') IS DISTINCT FROM 'object'
 OR r.config_snapshot->'v27_target_defect_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR r.config_snapshot->'v27_target_defect_prepare'->>'task_id' IS DISTINCT FROM p_task::text
 OR r.config_snapshot->'v27_target_defect_prepare'->>'input_digest' IS DISTINCT FROM p_digest
 OR r.config_snapshot->'v27_target_defect_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)
 OR jsonb_typeof(p_direct) IS DISTINCT FROM 'array' OR jsonb_array_length(p_direct)<>3
 OR NOT (p_direct @> jsonb_build_array(jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)))
 OR t.resource_class IS DISTINCT FROM 'cpu' OR p_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR r.config_snapshot ? 'v27_defect_prepare' OR r.config_snapshot ? 'v27_skin_prepare'
 OR r.config_snapshot ? 'v27_face_prepare' OR r.config_snapshot ? 'v27_detector_prepare' OR r.config_snapshot ? 'v27_person_prepare'
 OR r.config_snapshot->'v27_target_defect_prepare'->>'operation' IS DISTINCT FROM 'defects.propose'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((r.config_snapshot->'v27_target_defect_prepare'->>'target_namespace')::uuid,'semantic.defect.prepare:reference')
 OR r.config_snapshot->'v27_target_defect_prepare'->'direct_inputs' IS DISTINCT FROM p_direct
 OR r.config_snapshot->'v27_target_defect_prepare'->'input_entries' IS DISTINCT FROM p_rows
 OR (SELECT count(*) FROM jsonb_array_elements(p_rows) WHERE value->>'contract'='Params')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_rows) WHERE value->>'contract'='WorkingImage')<>1
 OR NOT p_rows @> jsonb_build_array(jsonb_build_object('contract','Params','ref',jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)))
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(p_direct) d WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_rows) e WHERE e.value->'ref'=d.value))
 OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_params_sha AND state='ready')
 OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array' OR jsonb_array_length(p_rows)<>p_count
 OR p_count NOT BETWEEN 3 AND 4097 OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array' THEN RETURN 'conflict';END IF;
 expected_transport:=p_chunks||jsonb_build_array(p_manifest);
 IF p_entries_sha IS DISTINCT FROM encode(public.digest(convert_to(photo.v27_target_manifest_json_text(p_rows),'UTF8'),'sha256'),'hex')
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(expected_transport) WITH ORDINALITY x(value,n)
 WHERE NOT EXISTS(SELECT 1 FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_task AND b.state='ready'
 AND b.artifact_id=(x.value->>'artifact_id')::uuid AND b.sha256=x.value->>'sha256'
 AND b.purpose=CASE WHEN x.n=jsonb_array_length(expected_transport) THEN 'v2_input_manifest' ELSE 'v2_input_chunk_'||(x.n-1)::text END
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=b.sha256 AND a.bytes=b.bytes
 AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.media_type='application/json' AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN 'conflict';END IF;
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
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN 'conflict';
END $$;
ALTER FUNCTION photo.semantic_target_defect_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_defect_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) FROM PUBLIC;
-- The raw helper is private; scheduler uses the strict binding envelope.

CREATE FUNCTION photo.semantic_target_defect_sealed_registration_binding_state(p_tenant uuid,p_run uuid,p_operation text,p_binding jsonb)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE transport jsonb;params jsonb;count_entries integer;ref jsonb;row_entry jsonb;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN ('defects.propose') THEN RETURN 'conflict';END IF;
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
 IF count_entries NOT BETWEEN 3 AND 4097 THEN RETURN 'conflict';END IF;
 IF jsonb_typeof(transport->'rows') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'rows')<>count_entries
 OR jsonb_typeof(transport->'chunks') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'chunks') NOT BETWEEN 1 AND 4097
 OR jsonb_typeof(transport->'direct') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'direct')<>3
 OR jsonb_typeof(transport->'manifest') IS DISTINCT FROM 'object' THEN RETURN 'conflict';END IF;
 FOR row_entry IN SELECT value FROM jsonb_array_elements(transport->'rows') LOOP
  IF jsonb_typeof(row_entry) IS DISTINCT FROM 'object' OR (row_entry-ARRAY['ref','contract'])<>'{}'
  OR row_entry->>'contract' IS NULL OR row_entry->>'contract' NOT IN ('Params','WorkingImage','FaceLandmarks','MaskRef','NpyArray') THEN RETURN 'conflict';END IF;
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
 RETURN photo.semantic_target_defect_sealed_registration_state(p_tenant,p_run,(p_binding->>'task_id')::uuid,
  (params->>'artifact_id')::uuid,params->>'sha256',p_binding->>'input_digest',p_binding->'profile',
  transport->'rows',transport->'manifest',transport->'chunks',transport->'direct',count_entries,transport->>'entries_sha');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN 'conflict';
END $$;
ALTER FUNCTION photo.semantic_target_defect_sealed_registration_binding_state(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_defect_sealed_registration_binding_state(uuid,uuid,text,jsonb) FROM PUBLIC;



CREATE FUNCTION photo.lock_semantic_target_defect_sealed_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE face jsonb;person jsonb;skin jsonb;c jsonb;n jsonb;parent_n jsonb;registration jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;project uuid;actor uuid;session_id uuid;k text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','config_text','person','landmarks','skin','policy','qualification','qualification_pin','params_pin','registration'])<>'{}'
 OR jsonb_typeof(p_packet->'config_text') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_packet->'policy') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_packet->'qualification') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 face:=p_packet->'landmarks'->'face';person:=p_packet->'person'->'target';skin:=p_packet->'skin';registration:=p_packet->'registration';
 IF face->>'operation' IS DISTINCT FROM 'face.landmarks' OR face->>'tenant_id' IS DISTINCT FROM p_tenant::text THEN RETURN false;END IF;
 project:=(face->>'project_id')::uuid;actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;
 c:=(p_packet->>'config_text')::jsonb;n:=c->'v27_target_defect_prepare';parent_n:=(face->>'config_text')::jsonb->'v27_target_face_prepare';
 IF photo.lock_v27_target_defect_sealed_graph_scope(p_tenant,p_run,(registration->>'task_id')::uuid) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR w.run_id IS NULL OR r.created_by IS DISTINCT FROM actor OR w.project_id IS DISTINCT FROM project
 OR c IS DISTINCT FROM r.config_snapshot OR c->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_packet->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR w.policy_sha IS DISTINCT FROM n->>'policy_sha'
 OR jsonb_typeof(n) IS DISTINCT FROM 'object' OR n->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (n-ARRAY['schema_version','operation','recovery','parent_run_id','request_sha','auth_session_id','target_ordinal','target_namespace','target_stage_id','manifest_sha','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','preparation_sha','qualification_sha','input_entries','input_sizes','direct_inputs','diameter_face_fraction','landmarks_run_id','landmarks_task_id','landmarks_result_id','landmarks_outputs','landmarks_params','skin_run_id','skin_task_id','skin_result_id','skin_outputs','skin_params','person_run_id','person_task_id','person_result_id','person_outputs','person_root','person_pixels'])<>'{}'
 OR n->>'auth_session_id' IS DISTINCT FROM session_id::text
 OR n->'recovery'->'qualification_report' IS DISTINCT FROM jsonb_build_object('artifact_id',p_packet->'qualification_pin'->'artifact_id','sha256',p_packet->'qualification_pin'->'sha256')
 OR n->'params' IS DISTINCT FROM registration->'params' OR n->'task_id' IS DISTINCT FROM registration->'task_id' OR n->'input_digest' IS DISTINCT FROM registration->'input_digest'
 OR n->'input_entries' IS DISTINCT FROM registration->'transport'->'rows' OR n->'direct_inputs' IS DISTINCT FROM registration->'transport'->'direct'
 OR n->'landmarks_run_id' IS DISTINCT FROM face->'run_id' OR n->'landmarks_task_id' IS DISTINCT FROM face->'registration'->'task_id'
 OR n->'landmarks_params' IS DISTINCT FROM face->'registration'->'params'
 OR n->'person_run_id' IS DISTINCT FROM person->'run_id' OR n->'person_task_id' IS DISTINCT FROM person->'registration'->'task_id'
 OR n->'person_root' IS DISTINCT FROM n->'person_outputs'->2 OR n->'person_pixels' IS DISTINCT FROM n->'person_outputs'->0
 OR n->'skin_result_id' IS DISTINCT FROM skin->'result_id' OR n->'skin_outputs' IS DISTINCT FROM skin->'outputs' OR n->'skin_params' IS DISTINCT FROM skin->'params'
 OR n->'skin_task_id' IS DISTINCT FROM skin->'admission'->'registration'->'task_id'
 OR n->'skin_run_id' IS DISTINCT FROM to_jsonb(photo.semantic_uuid_v5((n->>'target_namespace')::uuid,'semantic.mask.face_skin:reference'))
 OR n->'landmarks_outputs' IS DISTINCT FROM (SELECT outputs FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=(n->>'landmarks_task_id')::uuid AND id=(n->>'landmarks_result_id')::uuid)
 THEN RETURN false;END IF;
 FOR k IN SELECT unnest(ARRAY['parent_run_id','request_sha','auth_session_id','target_ordinal','target_namespace','manifest_sha','source','intent','snapshot','policy_sha']) LOOP
  IF n->k IS DISTINCT FROM parent_n->k OR n->k IS DISTINCT FROM ((person->>'config_text')::jsonb->'v27_target_person_prepare')->k
  OR n->k IS DISTINCT FROM ((skin->'admission'->>'config_text')::jsonb->'v27_target_skin_prepare')->k THEN RETURN false;END IF;
 END LOOP;
 IF photo.lock_semantic_target_person_result(p_tenant,(n->>'person_run_id')::uuid,p_packet->'person',(n->>'person_result_id')::uuid,n->'person_outputs') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_target_face_result(p_tenant,(n->>'landmarks_run_id')::uuid,p_packet->'landmarks',(n->>'landmarks_result_id')::uuid,n->'landmarks_outputs') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_target_skin_result(p_tenant,project,actor,session_id,(n->>'parent_run_id')::uuid,n->>'request_sha',(n->>'skin_task_id')::uuid,skin) IS NOT TRUE THEN RETURN false;END IF;
 IF p_packet->'policy' IS DISTINCT FROM (face->>'policy_text')::jsonb THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_qualification_policy_snapshot(p_tenant,project,actor,session_id,p_packet->'policy') IS NOT TRUE THEN RETURN false;END IF;
 IF p_packet->'qualification'->>'Operation' IS DISTINCT FROM 'defects.propose' OR p_packet->'qualification'->>'Backend' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_defect_qualification(p_tenant,project,actor,session_id,p_packet->'qualification',p_packet->'qualification_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_version_params(p_tenant,project,actor,session_id,(face->'binding'->>'asset_version_id')::uuid,(face->'binding'->'source'->>'artifact_id')::uuid,face->'binding'->'source'->>'sha256',(n->>'params_document')::uuid,(n->'params'->>'artifact_id')::uuid,n->'params'->>'sha256',p_packet->'params_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.semantic_target_defect_sealed_registration_binding_state(p_tenant,p_run,'defects.propose',registration) IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)=1
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_defect_sealed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_defect_sealed_evidence(uuid,uuid,jsonb) FROM PUBLIC;

CREATE FUNCTION photo.seal_semantic_target_defect_preparation(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;i photo.idempotency_records%ROWTYPE;t photo.tasks%ROWTYPE;actor uuid;session_id uuid;project uuid;ticket jsonb;next_cursor bigint;event_id uuid;admission_key text;changed integer;face jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432 OR p_evidence->'schema_version' IS DISTINCT FROM '1'::jsonb OR (p_evidence-ARRAY['schema_version','config_text','person','landmarks','skin','policy','qualification','qualification_pin','params_pin','registration'])<>'{}' OR jsonb_typeof(p_evidence->'landmarks'->'face') IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 face:=p_evidence->'landmarks'->'face';
 actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;project:=(face->>'project_id')::uuid;IF face->>'operation' IS NULL OR face->>'operation' IS DISTINCT FROM 'face.landmarks' THEN RETURN NULL;END IF;
 admission_key:='target-defect-prep:'||p_run::text;
 IF photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS NOT TRUE THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM actor OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic'
 OR face->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR (p_evidence->>'config_text')::jsonb IS DISTINCT FROM r.config_snapshot
 OR encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(face->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha THEN RETURN NULL;END IF;
 SELECT * INTO i FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=actor AND route='V2.Admit' AND idempotency_records.key=admission_key FOR UPDATE;
 IF NOT FOUND OR i.resource_id IS DISTINCT FROM p_run OR i.request_hash IS DISTINCT FROM face->>'request_sha' THEN RETURN NULL;END IF;
 ticket:=jsonb_build_object('run_id',p_run,'workflow_id',w.id,'state','queued');
 IF r.graph_sealed THEN
  IF i.response_status=202 AND i.response_body=ticket AND photo.lock_semantic_target_defect_sealed_evidence(p_tenant,p_run,p_evidence) IS TRUE THEN RETURN ticket;END IF;
  RETURN NULL;
 END IF;
 IF i.response_status IS NOT NULL OR i.expires_at<=clock_timestamp() THEN RETURN NULL;END IF;
 -- Native preparation currently has no consent-bearing admission contract.
 -- Reject links rather than silently bypassing generic consent epoch guards.
 IF EXISTS(SELECT 1 FROM photo.workflow_consent_links WHERE tenant_id=p_tenant AND workflow_id=w.id) THEN RETURN NULL;END IF;
 IF photo.lock_semantic_target_defect_admission_evidence(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_evidence->'registration'->>'task_id')::uuid FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version AND v2_enabled AND resource_class=t.resource_class AND backends ? 'cpu') THEN RETURN NULL;END IF;
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
ALTER FUNCTION photo.seal_semantic_target_defect_preparation(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_target_defect_preparation(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_semantic_target_defect_preparation(uuid,uuid,jsonb) TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
