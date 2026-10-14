-- +goose Up
-- Exact original-person consumption after completion; existing unique-candidate admission remains unchanged.
-- Additive SQL-only locks. No table, metadata rewrite, DML privilege or automatic activation.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_completed_person_authority(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_binding jsonb,p_locator jsonb,p_proof jsonb,p_policy jsonb,p_params jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE a jsonb;mask jsonb;params jsonb;expected jsonb;evidence jsonb;qualification jsonb;objects jsonb:='[]';f jsonb;o jsonb;ref jsonb;i integer;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR octet_length(p_proof::text)>33554432
 OR (p_proof-ARRAY['authority_text','params_text'])<>'{}'
 OR jsonb_typeof(p_proof->'authority_text') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_proof->'params_text') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_locator) IS DISTINCT FROM 'object' OR p_locator->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_locator-ARRAY['schema_version','root','pixels','qualification_report','authority_sha','files'])<>'{}'
 OR jsonb_typeof(p_locator->'files') IS DISTINCT FROM 'array' OR jsonb_array_length(p_locator->'files')<>2
 OR encode(public.digest(convert_to(p_proof->>'authority_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM p_locator->>'authority_sha'
 OR encode(public.digest(convert_to(p_proof->>'params_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM p_params->>'sha256'
 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status NOT IN('running','waiting_review','completed') OR NOT r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_defect_native_prepare';
 IF c->>'auth_session_id' IS DISTINCT FROM p_session::text OR c->'person_input' IS DISTINCT FROM p_locator
 OR c->'source' IS DISTINCT FROM p_binding OR c->'params' IS DISTINCT FROM p_params
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((c->>'parent_run_id')::uuid,'semantic.defect.prepare:reference') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(c->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.state<>'succeeded' OR t.cancel_requested OR t.operation<>'defects.propose' OR t.operation_version<>'1'
 OR t.canonical_result_id IS NULL OR t.params_artifact_id::text IS DISTINCT FROM p_params->>'artifact_id'
 OR t.input_digest IS DISTINCT FROM c->>'input_digest' THEN RETURN false;END IF;
 a:=(p_proof->>'authority_text')::jsonb;params:=(p_proof->>'params_text')::jsonb;
 IF jsonb_typeof(a) IS DISTINCT FROM 'object'
 OR (a-ARRAY['tenant','project','actor','session','bucket','binding','root','pixels','report','body','expected','evidence','qualification','objects','policy'])<>'{}'
 OR a->>'tenant' IS DISTINCT FROM p_tenant::text OR a->>'project' IS DISTINCT FROM p_project::text
 OR a->>'actor' IS DISTINCT FROM p_actor::text OR a->>'session' IS DISTINCT FROM p_session::text
 OR a->'binding' IS DISTINCT FROM p_binding OR (a->>'policy')::jsonb IS DISTINCT FROM p_policy
 OR a->'root' IS DISTINCT FROM p_locator->'root' OR a->'pixels' IS DISTINCT FROM p_locator->'pixels'
 OR a->'report' IS DISTINCT FROM p_locator->'qualification_report'
 OR a->'root'->>'artifact_id'=a->'pixels'->>'artifact_id'
 OR jsonb_typeof(a->'objects') IS DISTINCT FROM 'array' OR jsonb_array_length(a->'objects')<>2
 OR jsonb_typeof(a->'body') IS DISTINCT FROM 'string'
 OR encode(public.digest(convert_to(a->>'body','UTF8'),'sha256'),'hex') IS DISTINCT FROM a->'root'->>'sha256'
 THEN RETURN false;END IF;
 mask:=(a->>'body')::jsonb;expected:=(a->>'expected')::jsonb;evidence:=(a->>'evidence')::jsonb;qualification:=(a->>'qualification')::jsonb;
 IF mask->'artifact' IS DISTINCT FROM a->'pixels' OR mask->>'selector' IS DISTINCT FROM 'person'
 OR mask->'protected' IS DISTINCT FROM 'false'::jsonb OR mask->'object_id' IS DISTINCT FROM 'null'::jsonb
 OR mask->>'model_lock_id' IS DISTINCT FROM expected->>'ModelLockSHA'
 OR expected->>'Operation' IS DISTINCT FROM 'mask.person' OR expected->>'Version' IS DISTINCT FROM '1'
 OR params->'image' IS DISTINCT FROM p_binding->'source'
 OR params->'permanent' IS DISTINCT FROM (mask||jsonb_build_object('protected',true))
 OR params->'protections' IS DISTINCT FROM '[]'::jsonb
 OR qualification->>'artifact_id' IS DISTINCT FROM a->'report'->>'artifact_id'
 OR qualification->>'sha256' IS DISTINCT FROM a->'report'->>'sha256'
 OR jsonb_typeof(params->'faces') IS DISTINCT FROM 'array'
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(params->'faces') face WHERE face->'manual' IS NOT NULL AND face->'manual'<>'null'::jsonb)
 THEN RETURN false;END IF;
 FOR i IN 0..1 LOOP
  f:=p_locator->'files'->i;o:=a->'objects'->i;ref:=CASE i WHEN 0 THEN a->'root' ELSE a->'pixels' END;
  IF f IS DISTINCT FROM jsonb_build_object('role',CASE i WHEN 0 THEN 'person_root' ELSE 'person_pixels' END,
   'ref',ref,'bucket',a->'bucket','key',o->'Key','version',o->'VersionID','bytes',o->'Bytes','media',o->'ContentType')
   OR o->'SHA256' IS DISTINCT FROM ref->'sha256' THEN RETURN false;END IF;
  objects:=objects||jsonb_build_array(jsonb_build_object('artifact_id',ref->'artifact_id','sha256',ref->'sha256','bucket',a->'bucket',
   'key',o->'Key','version',o->'VersionID','bytes',o->'Bytes','media_type',o->'ContentType'));
 END LOOP;
 IF photo.lock_semantic_proposal_person_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_person_evidence(p_tenant,p_project,p_actor,p_session,(p_binding->>'asset_version_id')::uuid,
 (p_binding->'source'->>'artifact_id')::uuid,p_binding->'source'->>'sha256',(a->'root'->>'artifact_id')::uuid,a->'root'->>'sha256',
 (a->'pixels'->>'artifact_id')::uuid,a->'pixels'->>'sha256',expected,evidence) IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_person_qualification(p_tenant,p_project,p_actor,p_session,expected,qualification) IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_native_person_input_objects(p_tenant,p_project,p_actor,p_session,(p_binding->>'asset_version_id')::uuid,
 (p_binding->'source'->>'artifact_id')::uuid,p_binding->'source'->>'sha256',(a->'root'->>'artifact_id')::uuid,a->'root'->>'sha256',
 (a->'pixels'->>'artifact_id')::uuid,a->'pixels'->>'sha256',expected,objects) IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.semantic_proposal_person_profile_matches(p_tenant,p_project,p_actor,p_session,(p_binding->>'asset_version_id')::uuid,
 (p_binding->'source'->>'artifact_id')::uuid,p_binding->'source'->>'sha256',(a->'root'->>'artifact_id')::uuid,a->'root'->>'sha256',
 (a->'pixels'->>'artifact_id')::uuid,a->'pixels'->>'sha256',expected);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_completed_person_authority(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_completed_person_authority(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_original_defect_completed_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parser jsonb;face jsonb;result jsonb;config jsonb;native jsonb;parent_native jsonb;registration jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;actor uuid;session_id uuid;project uuid;key_name text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','landmarks','skin','config_text','policy','qualification','qualification_pin','params_pin','registration','person'])<>'{}'
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
 OR (native-ARRAY['recovery','schema_version','operation','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha','dependency_sha','input_entries','direct_inputs','landmarks_run_id','landmarks_task_id','landmarks_result_id','landmarks_outputs','landmarks_params','skin_run_id','skin_task_id','skin_result_id','skin_outputs','skin_params','person_input'])<>'{}'
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
 IF NOT (native ? 'person_input') OR NOT (p_packet ? 'person') THEN RETURN false;END IF;
 IF native ? 'person_input' THEN
  IF photo.lock_semantic_native_completed_person_authority(p_tenant,project,actor,session_id,p_run,native->'source',native->'person_input',p_packet->'person',p_packet->'policy',native->'params') IS NOT TRUE THEN RETURN false;END IF;
  IF (SELECT count(*) FROM jsonb_array_elements(native->'input_entries') entry
      WHERE entry->'ref'=native->'person_input'->'pixels' AND entry->>'contract'='NpyArray')<>1 THEN RETURN false;END IF;
 END IF;
 IF p_packet->'qualification'->>'Operation' IS DISTINCT FROM 'defects.propose' OR p_packet->'qualification'->>'Backend' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_defect_qualification(p_tenant,project,actor,session_id,p_packet->'qualification',p_packet->'qualification_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_version_params(p_tenant,project,actor,session_id,(face->'binding'->>'asset_version_id')::uuid,(face->'binding'->'source'->>'artifact_id')::uuid,face->'binding'->'source'->>'sha256',(native->>'params_document')::uuid,(native->'params'->>'artifact_id')::uuid,native->'params'->>'sha256',p_packet->'params_pin') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.semantic_native_defect_completed_binding_state(p_tenant,p_run,'defects.propose',registration) IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)=1
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run);

EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_original_defect_completed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_original_defect_completed_evidence(uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_original_defect_result(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_expected jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE admission jsonb;face jsonb;r photo.runs%ROWTYPE;found_result record;files jsonb;actual jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_expected) IS DISTINCT FROM 'object' OR octet_length(p_expected::text)>33554432
 OR (p_expected-ARRAY['schema_version','admission','result_id','outputs','params','files'])<>'{}'
 OR p_expected->'schema_version' IS DISTINCT FROM '1'::jsonb
 THEN RETURN false;END IF;
 admission:=p_expected->'admission';face:=admission->'landmarks'->'admission'->'face';
 IF face->>'tenant_id' IS DISTINCT FROM p_tenant::text OR face->>'project_id' IS DISTINCT FROM p_project::text
 OR face->>'actor_id' IS DISTINCT FROM p_actor::text OR face->>'session_id' IS DISTINCT FROM p_session::text
 OR face->>'parent_run_id' IS DISTINCT FROM p_parent::text OR face->>'request_sha' IS DISTINCT FROM p_request_sha THEN RETURN false;END IF;
 IF photo.lock_semantic_native_original_defect_completed_evidence(p_tenant,photo.semantic_uuid_v5(p_parent,'semantic.defect.prepare:reference'),admission) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5(p_parent,'semantic.defect.prepare:reference') FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=r.id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY attempt FOR SHARE;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY id FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND (created_by_task_id=p_task OR id IN(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task)) ORDER BY id FOR SHARE;
 PERFORM artifact_id FROM photo.output_uploads WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY artifact_id FOR SHARE;
 SELECT * INTO found_result FROM photo.semantic_native_defect_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND OR found_result.result_id::text IS DISTINCT FROM p_expected->>'result_id' THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('role',(ARRAY['defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report','params'])[o.ordinal+1],
 'ref',jsonb_build_object('artifact_id',o.artifact_id,'sha256',o.sha256),'bucket',o.bucket,'key',o.object_key,'version',o.object_version,'bytes',o.bytes,'media',o.media_type) ORDER BY o.ordinal)
 INTO files FROM photo.semantic_native_defect_result_objects(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task,found_result.result_id) o;
 IF jsonb_array_length(files) IS DISTINCT FROM 6 THEN RETURN false;END IF;
 actual:=jsonb_build_object('schema_version',1,'admission',admission,'result_id',found_result.result_id,'outputs',found_result.outputs,'params',found_result.params,'files',files);
 RETURN actual=p_expected;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_original_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_original_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_original_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_original_protection_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid,p_result uuid,p_anchor jsonb,p_root uuid,p_root_sha text,p_expected jsonb,p_completed jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF jsonb_typeof(p_expected) IS DISTINCT FROM 'array' OR octet_length(p_expected::text)>16384 THEN RETURN false;END IF;
 IF jsonb_array_length(p_expected)<>2
 OR photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE
 OR photo.lock_semantic_proposal_person_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR photo.lock_semantic_native_original_defect_result(p_tenant,p_project,p_actor,p_session,(p_anchor->>'parent_run_id')::uuid,p_anchor->>'request_sha',p_task,p_completed) IS NOT TRUE THEN RETURN false;END IF;
 PERFORM document_id FROM photo.semantic_native_protections WHERE tenant_id=p_tenant AND root_id=p_root AND root_sha=p_root_sha AND task_id=p_task AND result_id=p_result FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(p_root,(p_expected->1->>'artifact_id')::uuid) ORDER BY id FOR SHARE;
 PERFORM document_id FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND artifact_id=p_root ORDER BY document_id FOR SHARE;
 SELECT jsonb_agg(jsonb_build_object('ordinal',o.ordinal,'artifact_id',o.artifact_id,'sha256',o.sha256,'bucket',o.bucket,'key',o.object_key,'version',o.object_version,'bytes',o.bytes,'media_type',o.media_type) ORDER BY o.ordinal)
 INTO actual FROM photo.semantic_native_input_protection_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_task,p_result,p_anchor,p_root,p_root_sha) o;
 RETURN coalesce(actual=p_expected AND jsonb_array_length(actual)=2,false);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_original_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_original_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_original_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
