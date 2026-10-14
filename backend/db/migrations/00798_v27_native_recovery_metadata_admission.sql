-- +goose Up
-- Permit versioned server-generated recovery locators in original native configs.
-- Original config hash, producer, qualification, registration and scope gates remain.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_native_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;config jsonb;native jsonb;operation text;config_key text;project uuid;actor uuid;session_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432
 OR p_evidence->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_evidence->>'tenant_id' IS DISTINCT FROM p_tenant::text OR p_evidence->>'run_id' IS DISTINCT FROM p_run::text
 OR (p_evidence-ARRAY['schema_version','operation','tenant_id','project_id','actor_id','session_id','run_id','parent_run_id','request_sha','config_text','policy_text','anchor','binding','icc_sha','unit_rgb','source_pins','expected','qualification','params_document_id','params_pin','files','registration'])<>'{}'
 OR jsonb_typeof(p_evidence->'config_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'config_text') NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_evidence->'policy_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'policy_text') NOT BETWEEN 2 AND 4194304 THEN RETURN false;END IF;
 operation:=p_evidence->>'operation';
 IF operation IS NULL OR operation NOT IN ('face.detect','mask.person') THEN RETURN false;END IF;
 config_key:=CASE operation WHEN 'face.detect' THEN 'v27_detector_prepare' ELSE 'v27_person_prepare' END;
 project:=(p_evidence->>'project_id')::uuid;actor:=(p_evidence->>'actor_id')::uuid;session_id:=(p_evidence->>'session_id')::uuid;
 IF photo.lock_semantic_native_admission_anchor(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 config:=(p_evidence->>'config_text')::jsonb;native:=config->config_key;
 IF config IS DISTINCT FROM r.config_snapshot OR (config-ARRAY['project_id',config_key])<>'{}'
 OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(p_evidence->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['recovery','schema_version','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha'])<>'{}'
 OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))
 OR native->'parent_run_id' IS DISTINCT FROM p_evidence->'parent_run_id' OR native->'request_sha' IS DISTINCT FROM p_evidence->'request_sha'
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text OR native->>'policy_sha' IS DISTINCT FROM w.policy_sha
 OR native->'source' IS DISTINCT FROM p_evidence->'binding'
 OR native->'params_document' IS DISTINCT FROM p_evidence->'params_document_id'
 OR native->'params' IS DISTINCT FROM p_evidence->'registration'->'params'
 OR native->'task_id' IS DISTINCT FROM p_evidence->'registration'->'task_id'
 OR native->'input_digest' IS DISTINCT FROM p_evidence->'registration'->'input_digest' THEN RETURN false;END IF;
 IF photo.lock_semantic_native_source_authority(p_tenant,project,actor,session_id,p_evidence) IS NOT TRUE
 OR photo.semantic_native_registration_binding_state(p_tenant,p_run,operation,p_evidence->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_native_graph_budget(p_tenant,p_run,p_evidence);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_native_completed_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;config jsonb;native jsonb;operation text;config_key text;project uuid;actor uuid;session_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432
 OR p_evidence->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_evidence->>'tenant_id' IS DISTINCT FROM p_tenant::text OR p_evidence->>'run_id' IS DISTINCT FROM p_run::text
 OR (p_evidence-ARRAY['schema_version','operation','tenant_id','project_id','actor_id','session_id','run_id','parent_run_id','request_sha','config_text','policy_text','anchor','binding','icc_sha','unit_rgb','source_pins','expected','qualification','params_document_id','params_pin','files','registration'])<>'{}'
 OR jsonb_typeof(p_evidence->'config_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'config_text') NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_evidence->'policy_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'policy_text') NOT BETWEEN 2 AND 4194304 THEN RETURN false;END IF;
 operation:=p_evidence->>'operation';
 IF operation IS NULL OR operation NOT IN ('face.detect','mask.person') THEN RETURN false;END IF;
 config_key:=CASE operation WHEN 'face.detect' THEN 'v27_detector_prepare' ELSE 'v27_person_prepare' END;
 project:=(p_evidence->>'project_id')::uuid;actor:=(p_evidence->>'actor_id')::uuid;session_id:=(p_evidence->>'session_id')::uuid;
 IF photo.lock_semantic_native_admission_anchor(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 config:=(p_evidence->>'config_text')::jsonb;native:=config->config_key;
 IF config IS DISTINCT FROM r.config_snapshot OR (config-ARRAY['project_id',config_key])<>'{}'
 OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(p_evidence->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['recovery','schema_version','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha'])<>'{}'
 OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))
 OR native->'parent_run_id' IS DISTINCT FROM p_evidence->'parent_run_id' OR native->'request_sha' IS DISTINCT FROM p_evidence->'request_sha'
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text OR native->>'policy_sha' IS DISTINCT FROM w.policy_sha
 OR native->'source' IS DISTINCT FROM p_evidence->'binding'
 OR native->'params_document' IS DISTINCT FROM p_evidence->'params_document_id'
 OR native->'params' IS DISTINCT FROM p_evidence->'registration'->'params'
 OR native->'task_id' IS DISTINCT FROM p_evidence->'registration'->'task_id'
 OR native->'input_digest' IS DISTINCT FROM p_evidence->'registration'->'input_digest' THEN RETURN false;END IF;
 IF photo.lock_semantic_native_source_authority(p_tenant,project,actor,session_id,p_evidence) IS NOT TRUE
 OR photo.semantic_native_completed_binding_state(p_tenant,p_run,operation,p_evidence->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)=1
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_completed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_completed_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_completed_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_face_admission_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
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
 OR (native-ARRAY['recovery','schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','detector_run_id','detector_task_id','detector_result_id','detector_output','detector_params'])<>'{}'
 OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_face_completed_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
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
 OR (native-ARRAY['recovery','schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','detector_run_id','detector_task_id','detector_result_id','detector_output','detector_params'])<>'{}'
 OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_skin_admission_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parser jsonb;face jsonb;result jsonb;config jsonb;native jsonb;parent_native jsonb;registration jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;actor uuid;session_id uuid;project uuid;key_name text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','parser','parser_result','config_text','params_pin','registration'])<>'{}'
 OR jsonb_typeof(p_packet->'config_text') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 parser:=p_packet->'parser';face:=parser->'face';result:=p_packet->'parser_result';registration:=p_packet->'registration';
 IF face->>'operation' IS DISTINCT FROM 'face.parse' OR face->>'tenant_id' IS DISTINCT FROM p_tenant::text THEN RETURN false;END IF;
 project:=(face->>'project_id')::uuid;actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;
 IF photo.lock_semantic_face_completed_evidence(p_tenant,(face->>'run_id')::uuid,parser) IS NOT TRUE THEN RETURN false;END IF;
 config:=(p_packet->>'config_text')::jsonb;native:=config->'v27_skin_prepare';parent_native:=(face->>'config_text')::jsonb->'v27_face_prepare';
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR w.run_id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor
 OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2'
 OR config IS DISTINCT FROM r.config_snapshot OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_packet->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR w.policy_sha IS DISTINCT FROM native->>'policy_sha'
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['recovery','schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','parser_run_id','parser_task_id','parser_result_id','parser_outputs','parser_params'])<>'{}'
 OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))
 OR native->>'operation' IS DISTINCT FROM 'mask.face_skin'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.mask.face_skin:reference')
 OR native->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.mask.face_skin.params:reference')::text
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text
 OR native->'params' IS DISTINCT FROM registration->'params' OR native->'task_id' IS DISTINCT FROM registration->'task_id' OR native->'input_digest' IS DISTINCT FROM registration->'input_digest'
 OR native->'parser_run_id' IS DISTINCT FROM face->'run_id' OR native->'parser_task_id' IS DISTINCT FROM face->'registration'->'task_id'
 OR native->'parser_result_id' IS DISTINCT FROM result->'result_id' OR native->'parser_outputs' IS DISTINCT FROM result->'outputs' OR native->'parser_params' IS DISTINCT FROM result->'params'
 OR result->'params' IS DISTINCT FROM face->'registration'->'params'
 OR result->'detector' IS DISTINCT FROM parser->'detector_result' THEN RETURN false;END IF;
 FOR key_name IN SELECT unnest(ARRAY['parent_run_id','request_sha','source','intent','snapshot','policy_sha']) LOOP
  IF native->key_name IS DISTINCT FROM parent_native->key_name THEN RETURN false;END IF;
 END LOOP;
 IF photo.lock_semantic_native_parse_result(p_tenant,project,actor,session_id,(face->>'parent_run_id')::uuid,face->>'request_sha',(native->>'parser_task_id')::uuid,result) IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_version_params(p_tenant,project,actor,session_id,(face->'binding'->>'asset_version_id')::uuid,(face->'binding'->'source'->>'artifact_id')::uuid,face->'binding'->'source'->>'sha256',(native->>'params_document')::uuid,(native->'params'->>'artifact_id')::uuid,native->'params'->>'sha256',p_packet->'params_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.semantic_skin_registration_binding_state(p_tenant,p_run,'mask.face_skin',registration) IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_skin_graph_budget(p_tenant,p_run,jsonb_build_object('operation','mask.face_skin','actor_id',actor,'snapshot_id',w.input_snapshot_id,'registration',jsonb_build_object('task_id',native->'task_id')));
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_skin_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_skin_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_skin_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_skin_completed_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parser jsonb;face jsonb;result jsonb;config jsonb;native jsonb;parent_native jsonb;registration jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;actor uuid;session_id uuid;project uuid;key_name text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','parser','parser_result','config_text','params_pin','registration'])<>'{}'
 OR jsonb_typeof(p_packet->'config_text') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 parser:=p_packet->'parser';face:=parser->'face';result:=p_packet->'parser_result';registration:=p_packet->'registration';
 IF face->>'operation' IS DISTINCT FROM 'face.parse' OR face->>'tenant_id' IS DISTINCT FROM p_tenant::text THEN RETURN false;END IF;
 project:=(face->>'project_id')::uuid;actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;
 IF photo.lock_semantic_face_completed_evidence(p_tenant,(face->>'run_id')::uuid,parser) IS NOT TRUE THEN RETURN false;END IF;
 config:=(p_packet->>'config_text')::jsonb;native:=config->'v27_skin_prepare';parent_native:=(face->>'config_text')::jsonb->'v27_face_prepare';
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR w.run_id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor
 OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2'
 OR config IS DISTINCT FROM r.config_snapshot OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_packet->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR w.policy_sha IS DISTINCT FROM native->>'policy_sha'
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['recovery','schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','parser_run_id','parser_task_id','parser_result_id','parser_outputs','parser_params'])<>'{}'
 OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))
 OR native->>'operation' IS DISTINCT FROM 'mask.face_skin'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.mask.face_skin:reference')
 OR native->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.mask.face_skin.params:reference')::text
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text
 OR native->'params' IS DISTINCT FROM registration->'params' OR native->'task_id' IS DISTINCT FROM registration->'task_id' OR native->'input_digest' IS DISTINCT FROM registration->'input_digest'
 OR native->'parser_run_id' IS DISTINCT FROM face->'run_id' OR native->'parser_task_id' IS DISTINCT FROM face->'registration'->'task_id'
 OR native->'parser_result_id' IS DISTINCT FROM result->'result_id' OR native->'parser_outputs' IS DISTINCT FROM result->'outputs' OR native->'parser_params' IS DISTINCT FROM result->'params'
 OR result->'params' IS DISTINCT FROM face->'registration'->'params'
 OR result->'detector' IS DISTINCT FROM parser->'detector_result' THEN RETURN false;END IF;
 FOR key_name IN SELECT unnest(ARRAY['parent_run_id','request_sha','source','intent','snapshot','policy_sha']) LOOP
  IF native->key_name IS DISTINCT FROM parent_native->key_name THEN RETURN false;END IF;
 END LOOP;
 IF photo.lock_semantic_native_parse_result(p_tenant,project,actor,session_id,(face->>'parent_run_id')::uuid,face->>'request_sha',(native->>'parser_task_id')::uuid,result) IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_version_params(p_tenant,project,actor,session_id,(face->'binding'->>'asset_version_id')::uuid,(face->'binding'->'source'->>'artifact_id')::uuid,face->'binding'->'source'->>'sha256',(native->>'params_document')::uuid,(native->'params'->>'artifact_id')::uuid,native->'params'->>'sha256',p_packet->'params_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.semantic_skin_completed_binding_state(p_tenant,p_run,'mask.face_skin',registration) IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)=1
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run);

EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_skin_completed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_skin_completed_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_skin_completed_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_native_defect_admission_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parser jsonb;face jsonb;result jsonb;config jsonb;native jsonb;parent_native jsonb;registration jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;actor uuid;session_id uuid;project uuid;key_name text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','landmarks','skin','config_text','policy','qualification','qualification_pin','params_pin','registration'])<>'{}'
 OR jsonb_typeof(p_packet->'policy') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_packet->'qualification') IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_packet->'config_text') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 parser:=p_packet->'landmarks';face:=parser->'admission'->'face';result:=p_packet->'skin';registration:=p_packet->'registration';
 IF face->>'operation' IS DISTINCT FROM 'face.landmarks' OR face->>'tenant_id' IS DISTINCT FROM p_tenant::text THEN RETURN false;END IF;
 project:=(face->>'project_id')::uuid;actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;
 config:=(p_packet->>'config_text')::jsonb;native:=config->'v27_defect_native_prepare';parent_native:=(face->>'config_text')::jsonb->'v27_face_prepare';
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR w.run_id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor
 OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2'
 OR config IS DISTINCT FROM r.config_snapshot OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_packet->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR w.policy_sha IS DISTINCT FROM native->>'policy_sha'
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['recovery','schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','dependency_sha','input_entries','direct_inputs','landmarks_run_id','landmarks_task_id','landmarks_result_id','landmarks_outputs','landmarks_params','skin_run_id','skin_task_id','skin_result_id','skin_outputs','skin_params'])<>'{}'
 OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))
 OR native->>'operation' IS DISTINCT FROM 'defects.propose'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.defect.prepare:reference')
 OR native->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.defect.params:reference')::text
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text
 OR native->'params' IS DISTINCT FROM registration->'params' OR native->'task_id' IS DISTINCT FROM registration->'task_id' OR native->'input_digest' IS DISTINCT FROM registration->'input_digest'
 OR native->'landmarks_run_id' IS DISTINCT FROM face->'run_id'
 OR native->'landmarks_task_id' IS DISTINCT FROM face->'registration'->'task_id'
 OR native->'landmarks_result_id' IS DISTINCT FROM parser->'result_id'
 OR native->'landmarks_outputs' IS DISTINCT FROM parser->'outputs'
 OR native->'landmarks_params' IS DISTINCT FROM parser->'params'
 OR native->'skin_run_id' IS DISTINCT FROM to_jsonb(photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.mask.face_skin:reference'))
 OR native->'skin_task_id' IS DISTINCT FROM result->'admission'->'registration'->'task_id'
 OR native->'skin_result_id' IS DISTINCT FROM result->'result_id'
 OR native->'skin_outputs' IS DISTINCT FROM result->'outputs'
 OR native->'skin_params' IS DISTINCT FROM result->'params'
 OR native->'direct_inputs' IS DISTINCT FROM registration->'transport'->'direct'
 OR native->'input_entries' IS DISTINCT FROM registration->'transport'->'rows'
 OR result->'admission'->'parser'->'face'->>'parent_run_id' IS DISTINCT FROM face->>'parent_run_id'
 OR result->'admission'->'parser'->'face'->>'request_sha' IS DISTINCT FROM face->>'request_sha'
 THEN RETURN false;END IF;
 FOR key_name IN SELECT unnest(ARRAY['parent_run_id','request_sha','source','intent','snapshot','policy_sha']) LOOP
  IF native->key_name IS DISTINCT FROM parent_native->key_name THEN RETURN false;END IF;
 END LOOP;
 IF photo.lock_semantic_native_landmark_result(p_tenant,project,actor,session_id,(face->>'parent_run_id')::uuid,face->>'request_sha',(native->>'landmarks_task_id')::uuid,parser) IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_native_skin_result(p_tenant,project,actor,session_id,(face->>'parent_run_id')::uuid,face->>'request_sha',(native->>'skin_task_id')::uuid,result) IS NOT TRUE THEN RETURN false;END IF;
 IF p_packet->'policy' IS DISTINCT FROM (face->>'policy_text')::jsonb THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_qualification_policy_snapshot(p_tenant,project,actor,session_id,p_packet->'policy') IS NOT TRUE THEN RETURN false;END IF;
 IF p_packet->'qualification'->>'Operation' IS DISTINCT FROM 'defects.propose' OR p_packet->'qualification'->>'Backend' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_defect_qualification(p_tenant,project,actor,session_id,p_packet->'qualification',p_packet->'qualification_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_version_params(p_tenant,project,actor,session_id,(face->'binding'->>'asset_version_id')::uuid,(face->'binding'->'source'->>'artifact_id')::uuid,face->'binding'->'source'->>'sha256',(native->>'params_document')::uuid,(native->'params'->>'artifact_id')::uuid,native->'params'->>'sha256',p_packet->'params_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.semantic_native_defect_registration_binding_state(p_tenant,p_run,'defects.propose',registration) IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_native_defect_graph_budget(p_tenant,p_run,jsonb_build_object('operation','defects.propose','actor_id',actor,'snapshot_id',w.input_snapshot_id,'registration',jsonb_build_object('task_id',native->'task_id')));
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_native_defect_completed_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parser jsonb;face jsonb;result jsonb;config jsonb;native jsonb;parent_native jsonb;registration jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;actor uuid;session_id uuid;project uuid;key_name text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','landmarks','skin','config_text','policy','qualification','qualification_pin','params_pin','registration'])<>'{}'
 OR jsonb_typeof(p_packet->'policy') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_packet->'qualification') IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_packet->'config_text') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 parser:=p_packet->'landmarks';face:=parser->'admission'->'face';result:=p_packet->'skin';registration:=p_packet->'registration';
 IF face->>'operation' IS DISTINCT FROM 'face.landmarks' OR face->>'tenant_id' IS DISTINCT FROM p_tenant::text THEN RETURN false;END IF;
 project:=(face->>'project_id')::uuid;actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;
 config:=(p_packet->>'config_text')::jsonb;native:=config->'v27_defect_native_prepare';parent_native:=(face->>'config_text')::jsonb->'v27_face_prepare';
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR w.run_id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor
 OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2'
 OR config IS DISTINCT FROM r.config_snapshot OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_packet->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR w.policy_sha IS DISTINCT FROM native->>'policy_sha'
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['recovery','schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','dependency_sha','input_entries','direct_inputs','landmarks_run_id','landmarks_task_id','landmarks_result_id','landmarks_outputs','landmarks_params','skin_run_id','skin_task_id','skin_result_id','skin_outputs','skin_params'])<>'{}'
 OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))
 OR native->>'operation' IS DISTINCT FROM 'defects.propose'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.defect.prepare:reference')
 OR native->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.defect.params:reference')::text
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text
 OR native->'params' IS DISTINCT FROM registration->'params' OR native->'task_id' IS DISTINCT FROM registration->'task_id' OR native->'input_digest' IS DISTINCT FROM registration->'input_digest'
 OR native->'landmarks_run_id' IS DISTINCT FROM face->'run_id'
 OR native->'landmarks_task_id' IS DISTINCT FROM face->'registration'->'task_id'
 OR native->'landmarks_result_id' IS DISTINCT FROM parser->'result_id'
 OR native->'landmarks_outputs' IS DISTINCT FROM parser->'outputs'
 OR native->'landmarks_params' IS DISTINCT FROM parser->'params'
 OR native->'skin_run_id' IS DISTINCT FROM to_jsonb(photo.semantic_uuid_v5((face->>'parent_run_id')::uuid,'semantic.mask.face_skin:reference'))
 OR native->'skin_task_id' IS DISTINCT FROM result->'admission'->'registration'->'task_id'
 OR native->'skin_result_id' IS DISTINCT FROM result->'result_id'
 OR native->'skin_outputs' IS DISTINCT FROM result->'outputs'
 OR native->'skin_params' IS DISTINCT FROM result->'params'
 OR native->'direct_inputs' IS DISTINCT FROM registration->'transport'->'direct'
 OR native->'input_entries' IS DISTINCT FROM registration->'transport'->'rows'
 OR result->'admission'->'parser'->'face'->>'parent_run_id' IS DISTINCT FROM face->>'parent_run_id'
 OR result->'admission'->'parser'->'face'->>'request_sha' IS DISTINCT FROM face->>'request_sha'
 THEN RETURN false;END IF;
 FOR key_name IN SELECT unnest(ARRAY['parent_run_id','request_sha','source','intent','snapshot','policy_sha']) LOOP
  IF native->key_name IS DISTINCT FROM parent_native->key_name THEN RETURN false;END IF;
 END LOOP;
 IF photo.lock_semantic_native_landmark_result(p_tenant,project,actor,session_id,(face->>'parent_run_id')::uuid,face->>'request_sha',(native->>'landmarks_task_id')::uuid,parser) IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_native_skin_result(p_tenant,project,actor,session_id,(face->>'parent_run_id')::uuid,face->>'request_sha',(native->>'skin_task_id')::uuid,result) IS NOT TRUE THEN RETURN false;END IF;
 IF p_packet->'policy' IS DISTINCT FROM (face->>'policy_text')::jsonb THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_qualification_policy_snapshot(p_tenant,project,actor,session_id,p_packet->'policy') IS NOT TRUE THEN RETURN false;END IF;
 IF p_packet->'qualification'->>'Operation' IS DISTINCT FROM 'defects.propose' OR p_packet->'qualification'->>'Backend' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_defect_qualification(p_tenant,project,actor,session_id,p_packet->'qualification',p_packet->'qualification_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_version_params(p_tenant,project,actor,session_id,(face->'binding'->>'asset_version_id')::uuid,(face->'binding'->'source'->>'artifact_id')::uuid,face->'binding'->'source'->>'sha256',(native->>'params_document')::uuid,(native->'params'->>'artifact_id')::uuid,native->'params'->>'sha256',p_packet->'params_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.semantic_native_defect_completed_binding_state(p_tenant,p_run,'defects.propose',registration) IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)=1
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run);

EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_defect_completed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_defect_completed_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_defect_completed_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
