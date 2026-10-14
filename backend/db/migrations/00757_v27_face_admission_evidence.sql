-- +goose Up
-- Exact native graph and live reservation, retained after all evidence locks.
-- Does not activate, seal or publish a queued event.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_face_graph_budget(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;w photo.v2_workflows%ROWTYPE;budget numeric;matched integer:=0;candidate record;
BEGIN
 IF jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432
 OR p_evidence->>'operation' IS NULL OR p_evidence->>'operation' NOT IN ('face.landmarks','face.parse') THEN RETURN false;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.failure_policy<>'fail_fast' OR r.created_by::text IS DISTINCT FROM p_evidence->>'actor_id' THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' OR w.input_snapshot_id::text IS DISTINCT FROM (p_evidence->'anchor'->>'intent_text')::jsonb->>'target_snapshot_id' THEN RETURN false;END IF;
 FOR candidate IN SELECT id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR UPDATE LOOP
  matched:=matched+1;
  IF candidate.id::text IS DISTINCT FROM p_evidence->'registration'->>'task_id' THEN RETURN false;END IF;
 END LOOP;
 IF matched<>1 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_evidence->'registration'->>'task_id')::uuid;
 IF NOT FOUND OR t.state<>'blocked' OR t.cancel_requested OR t.attempt<>0 OR t.operation IS DISTINCT FROM p_evidence->>'operation' OR t.operation_version<>'1'
 OR t.resource_class<>'gpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cuda' OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN false;END IF;
 -- Native outputs retain their established roles. Generic V2 document
 -- output policy / v2_enabled activation are separate gateway/seal obligations.
 IF NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation AND op.version=t.operation_version AND op.resource_class=t.resource_class AND op.backends ? (t.execution_profile->>'backend')) THEN RETURN false;END IF;
 IF t.operation='face.landmarks' THEN
  IF (SELECT count(*) FROM photo.operation_output_policies WHERE operation=t.operation AND version=t.operation_version)<>1
  OR NOT EXISTS(SELECT 1 FROM photo.operation_output_policies WHERE operation=t.operation AND version=t.operation_version AND role='face_landmarks' AND media_types='["application/json"]'::jsonb AND max_count=1 AND max_bytes>0) THEN RETURN false;END IF;
 ELSE
  IF (SELECT count(*) FROM photo.operation_output_policies WHERE operation=t.operation AND version=t.operation_version)<>2
  OR EXISTS(SELECT 1 FROM (VALUES ('face_labels','application/octet-stream'),('face_labels_ref','application/json')) expected(role,media)
   WHERE NOT EXISTS(SELECT 1 FROM photo.operation_output_policies policy WHERE policy.operation=t.operation AND policy.version=t.operation_version AND policy.role=expected.role AND policy.media_types=jsonb_build_array(expected.media) AND policy.max_count=1 AND policy.max_bytes>0)) THEN RETURN false;END IF;
 END IF;
 IF jsonb_typeof(t.execution_profile->'resource_estimate'->'output_bytes') IS DISTINCT FROM 'number' OR (t.execution_profile->'resource_estimate'->>'output_bytes') !~ '^[0-9]+$' THEN RETURN false;END IF;
 budget:=(t.execution_profile->'resource_estimate'->>'output_bytes')::numeric;
 IF budget NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 matched:=0;
 FOR q IN SELECT * FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run ORDER BY id FOR UPDATE LOOP
  matched:=matched+1;
  IF q.state<>'reserved' OR q.expires_at<=clock_timestamp() OR q.principal_type<>'user' OR q.principal_id IS DISTINCT FROM r.created_by OR q.files<>0
  OR q.bytes::numeric<budget+8388608 OR q.bytes>9007199254740991
  OR NOT EXISTS(SELECT 1 FROM photo.tenant_usage WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes) THEN RETURN false;END IF;
 END LOOP;
 RETURN matched=1 AND EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND status='draft' AND NOT cancel_requested AND NOT graph_sealed)
 AND EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=t.id AND state='blocked' AND NOT cancel_requested);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_face_graph_budget(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_face_graph_budget(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_face_graph_budget(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- Original completed detector authority and downstream closure/qualification
-- are retained together. This barrier neither seals nor activates a graph.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_face_admission_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
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
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
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
 OR photo.semantic_face_registration_binding_state(p_tenant,p_run,operation,face->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_face_graph_budget(p_tenant,p_run,face);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_face_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_face_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_face_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
