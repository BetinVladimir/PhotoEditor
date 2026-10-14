-- +goose Up
-- Qualified target face admission, no activation.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_target_face_admission_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
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
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
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
 OR photo.semantic_target_face_registration_binding_state(p_tenant,p_run,operation,face->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_face_graph_budget(p_tenant,p_run,face);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_face_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_face_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_target_face_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
