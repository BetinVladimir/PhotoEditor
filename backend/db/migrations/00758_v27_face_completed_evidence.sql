-- +goose Up
-- Native FAN/BiSeNet sealed registration replay gate; caller retains all source, policy and input authority locks.
-- Never activates a task or repairs partial state.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_face_completed_registration_state(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_params_sha text,p_digest text,p_profile jsonb,p_rows jsonb,p_manifest jsonb,p_chunks jsonb,p_direct jsonb,p_count integer,p_entries_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE; actual jsonb; transport jsonb; expected_transport jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'conflict';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed
 OR t.operation NOT IN ('face.landmarks','face.parse') OR t.resource_class<>'gpu' OR t.operation_version<>'1'
 OR jsonb_typeof(r.config_snapshot->'v27_face_prepare') IS DISTINCT FROM 'object'
 OR r.config_snapshot->'v27_face_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR r.config_snapshot->'v27_face_prepare'->>'operation' IS DISTINCT FROM t.operation
 OR p_profile->>'backend' IS DISTINCT FROM 'cuda'
 OR r.config_snapshot ? 'v27_detector_prepare' OR r.config_snapshot ? 'v27_person_prepare' OR r.config_snapshot ? 'v27_defect_prepare'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((r.config_snapshot->'v27_face_prepare'->>'parent_run_id')::uuid,'semantic.'||t.operation||':reference')
 OR r.config_snapshot->'v27_face_prepare'->>'task_id' IS DISTINCT FROM p_task::text
 OR r.config_snapshot->'v27_face_prepare'->>'input_digest' IS DISTINCT FROM p_digest
 OR r.config_snapshot->'v27_face_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)
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
 OR NOT p_rows @> jsonb_build_array(jsonb_build_object('contract','WorkingImage','ref',r.config_snapshot->'v27_face_prepare'->'source'->'source'))
 OR NOT p_rows @> jsonb_build_array(jsonb_build_object('contract','FaceBoxes','ref',r.config_snapshot->'v27_face_prepare'->'detector_output'))
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
ALTER FUNCTION photo.semantic_face_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_face_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_face_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) TO photo_scheduler;

-- Strict sealed native FAN/BiSeNet registration envelope. No activation or model authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_face_completed_binding_state(p_tenant uuid,p_run uuid,p_operation text,p_binding jsonb)
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
 IF count_entries NOT BETWEEN 4 AND 4097 THEN RETURN 'conflict';END IF;
 IF jsonb_typeof(transport->'rows') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'rows')<>count_entries
 OR jsonb_typeof(transport->'chunks') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'chunks') NOT BETWEEN 1 AND 4097
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
 RETURN photo.semantic_face_completed_registration_state(p_tenant,p_run,(p_binding->>'task_id')::uuid,
  (params->>'artifact_id')::uuid,params->>'sha256',p_binding->>'input_digest',p_binding->'profile',
  transport->'rows',transport->'manifest',transport->'chunks',transport->'direct',count_entries,transport->>'entries_sha');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN 'conflict';
END $$;
ALTER FUNCTION photo.semantic_face_completed_binding_state(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_face_completed_binding_state(uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_face_completed_binding_state(uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd


-- Original completed detector authority and downstream closure/qualification
-- are retained together. This barrier neither seals nor activates a graph.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_face_completed_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE face jsonb;detector jsonb;result jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 config jsonb;native jsonb;parent_native jsonb;operation text;project uuid;actor uuid;session_id uuid;
 key_name text;files jsonb;rows jsonb;registration jsonb;output_pin jsonb;qualified boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','face','detector','detector_result'])<>'{}' THEN RETURN false;END IF;
 face:=p_packet->'face';detector:=p_packet->'detector';result:=p_packet->'detector_result';
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
 IF photo.lock_semantic_native_completed_evidence(p_tenant,(detector->>'run_id')::uuid,detector) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 config:=(face->>'config_text')::jsonb;native:=config->'v27_face_prepare';parent_native:=(detector->>'config_text')::jsonb->'v27_detector_prepare';
 IF config IS DISTINCT FROM r.config_snapshot OR (config-ARRAY['project_id','v27_face_prepare'])<>'{}'
 OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(face->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(face->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','detector_run_id','detector_task_id','detector_result_id','detector_output','detector_params'])<>'{}'
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
 IF photo.lock_semantic_native_detector_result(p_tenant,project,actor,session_id,(face->>'parent_run_id')::uuid,face->>'request_sha',(native->>'detector_task_id')::uuid,result) IS NOT TRUE THEN RETURN false;END IF;
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
 OR photo.semantic_face_completed_binding_state(p_tenant,p_run,operation,face->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)=1
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_face_completed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_face_completed_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_face_completed_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
