-- +goose Up
-- Composes original qualified parser authority with exact CPU skin registration.
-- No activation, receipt or object publication.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_skin_admission_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
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
 OR (native-ARRAY['schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','parser_run_id','parser_task_id','parser_result_id','parser_outputs','parser_params'])<>'{}'
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
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
