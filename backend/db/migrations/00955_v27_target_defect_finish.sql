-- +goose Up
-- Exact completed consumer metadata and one-time target defect quota release.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_target_defect_completed_output_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_outputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;c jsonb;ref jsonb;seen uuid[]:='{}';aid uuid;i integer;roles text[];media text[];
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.state<>'succeeded' OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' OR t.cancel_requested
 OR p_attempt IS DISTINCT FROM t.attempt OR p_fence IS DISTINCT FROM t.fencing_token OR p_attempt<1 OR p_fence<1 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.status NOT IN('running','completed') THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_target_defect_prepare';
 IF t.operation IS DISTINCT FROM 'defects.propose' THEN RETURN false;END IF;
 roles:=ARRAY['defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report'];media:=ARRAY['application/octet-stream','application/octet-stream','image/png','application/json','application/json'];
 IF c->>'operation' IS DISTINCT FROM t.operation
 OR r.id IS DISTINCT FROM photo.semantic_uuid_v5((c->>'target_namespace')::uuid,'semantic.defect.prepare:reference')
 OR r.config_snapshot ? 'v27_defect_prepare' OR r.config_snapshot ? 'v27_skin_prepare'
 OR r.config_snapshot ? 'v27_face_prepare' OR r.config_snapshot ? 'v27_detector_prepare' OR r.config_snapshot ? 'v27_person_prepare' THEN RETURN false;END IF;
 IF t.id IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(r.id,'workflow'),'defects.propose:0') OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN false;END IF;
 IF c->'schema_version' IS DISTINCT FROM '1'::jsonb OR c->>'task_id' IS DISTINCT FROM t.id::text OR c->>'input_digest' IS DISTINCT FROM t.input_digest
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND project_id=(r.config_snapshot->>'project_id')::uuid AND renderer_version='v2.2' AND workflow_type='semantic')
 OR jsonb_typeof(p_outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(p_outputs)<>cardinality(roles) OR octet_length(p_outputs::text)>8192 THEN RETURN false;END IF;
 FOR i IN 0..cardinality(roles)-1 LOOP
  ref:=p_outputs->i;
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid IS NULL OR aid='00000000-0000-0000-0000-000000000000'::uuid OR aid=ANY(seen) OR aid=t.params_artifact_id THEN RETURN false;END IF;
  seen:=array_append(seen,aid);
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
  WHERE a.tenant_id=p_tenant AND a.id=aid AND a.sha256=ref->>'sha256' AND a.output_role=roles[i+1] AND a.media_type=media[i+1]
  AND a.bytes>0 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
  AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready' AND u.completed_at IS NOT NULL
  AND u.role=a.output_role AND u.bytes=a.bytes AND u.sha256=a.sha256 AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version AND u.media_type=a.media_type) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_target_defect_completed_output_ready(uuid,uuid,integer,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_defect_completed_output_ready(uuid,uuid,integer,bigint,jsonb) FROM PUBLIC;
CREATE OR REPLACE FUNCTION photo.lock_v27_target_defect_sealed_graph_scope(p_tenant uuid,p_run uuid,p_task uuid)
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
 OR r.kind<>'v2' OR r.status NOT IN('queued','running','completed') OR NOT r.graph_sealed OR r.cancel_requested
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
CREATE OR REPLACE FUNCTION photo.semantic_target_defect_sealed_registration_state(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_params_sha text,p_digest text,p_profile jsonb,p_rows jsonb,p_manifest jsonb,p_chunks jsonb,p_direct jsonb,p_count integer,p_entries_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE; actual jsonb; transport jsonb; expected_transport jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'conflict';END IF;
 IF photo.lock_v27_target_defect_sealed_graph_scope(p_tenant,p_run,p_task) IS NOT TRUE THEN RETURN 'conflict';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('queued','running','completed') OR NOT r.graph_sealed OR r.cancel_requested
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

CREATE OR REPLACE FUNCTION photo.lock_semantic_target_defect_sealed_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
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

CREATE FUNCTION photo.finish_semantic_target_defect(p_tenant uuid,p_run uuid,p_packet jsonb,p_result uuid,p_outputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;q photo.quota_reservations%ROWTYPE;next_cursor bigint;event_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.lock_semantic_target_defect_sealed_evidence(p_tenant,p_run,p_packet) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_packet->'registration'->>'task_id')::uuid FOR UPDATE;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=t.canonical_result_id AND attempt=t.attempt AND fencing_token=t.fencing_token FOR SHARE;
 IF z.id IS NULL OR p_result IS DISTINCT FROM z.id OR p_outputs IS DISTINCT FROM z.outputs
 OR photo.semantic_target_defect_completed_output_ready(p_tenant,t.id,t.attempt,t.fencing_token,z.outputs) IS NOT TRUE THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run)<>1 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF q.principal_type<>'user' OR q.principal_id IS DISTINCT FROM r.created_by OR q.files<>0 THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks task ON task.tenant_id=u.tenant_id AND task.id=u.task_id WHERE task.tenant_id=p_tenant AND task.run_id=p_run AND u.state NOT IN('ready','cleaned'))
 OR EXISTS(SELECT 1 FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND state NOT IN('ready','cleaned')) THEN RETURN false;END IF;
 IF r.status='completed' THEN RETURN q.state='released' AND q.bytes=0;END IF;
 IF r.status<>'running' OR r.revision>=9007199254740991 OR q.state<>'reserved' OR q.expires_at<=clock_timestamp() THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'TARGET_DEFECT_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=q.id;
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run AND status='running' AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'TARGET_DEFECT_FINISH_RACE' USING ERRCODE='40001';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(p_tenant,next_cursor,p_run,r.created_by,'principal','v2.run.completed',jsonb_build_object('run_id',p_run,'status','completed','revision',r.revision+1));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(p_tenant,event_id,'photo.events.v1','v2.run.completed',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.completed','tenant_id',p_tenant,'run_id',p_run,'status','completed'));
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.finish_semantic_target_defect(uuid,uuid,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_semantic_target_defect(uuid,uuid,jsonb,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_semantic_target_defect(uuid,uuid,jsonb,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
