-- +goose Up
-- Dedicated target defect admission. No activation, receipt or backfill.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_target_defect_graph_budget(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;w photo.v2_workflows%ROWTYPE;budget numeric;matched integer:=0;candidate record;
BEGIN
 IF jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>16384
 OR (p_evidence-'operation'-'actor_id'-'snapshot_id'-'registration')<>'{}'
 OR jsonb_typeof(p_evidence->'actor_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_evidence->'snapshot_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_evidence->'registration') IS DISTINCT FROM 'object'
 OR ((p_evidence->'registration')-'task_id')<>'{}'
 OR jsonb_typeof(p_evidence->'registration'->'task_id') IS DISTINCT FROM 'string'
 OR p_evidence->>'operation' IS NULL OR p_evidence->>'operation' NOT IN ('defects.propose') THEN RETURN false;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.failure_policy<>'fail_fast' OR r.created_by::text IS DISTINCT FROM p_evidence->>'actor_id' THEN RETURN false;END IF;
 IF jsonb_typeof(r.config_snapshot->'v27_target_defect_prepare') IS DISTINCT FROM 'object'
 OR r.config_snapshot->'v27_target_defect_prepare'->>'operation' IS DISTINCT FROM 'defects.propose'
 OR r.config_snapshot->'v27_target_defect_prepare'->>'task_id' IS DISTINCT FROM p_evidence->'registration'->>'task_id'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((r.config_snapshot->'v27_target_defect_prepare'->>'target_namespace')::uuid,'semantic.defect.prepare:reference') THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' OR w.input_snapshot_id::text IS DISTINCT FROM p_evidence->>'snapshot_id' THEN RETURN false;END IF;
 FOR candidate IN SELECT id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR UPDATE LOOP
  matched:=matched+1;
  IF candidate.id::text IS DISTINCT FROM p_evidence->'registration'->>'task_id' THEN RETURN false;END IF;
 END LOOP;
 IF matched<>1 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_evidence->'registration'->>'task_id')::uuid;
 IF NOT FOUND OR t.state<>'blocked' OR t.cancel_requested OR t.attempt<>0 OR t.operation IS DISTINCT FROM p_evidence->>'operation' OR t.operation_version<>'1'
 OR t.resource_class<>'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN false;END IF;
 -- Native outputs retain their established roles. Generic V2 document
 -- output policy / v2_enabled activation are separate gateway/seal obligations.
 IF NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation AND op.version=t.operation_version AND op.resource_class=t.resource_class AND op.backends ? (t.execution_profile->>'backend')) THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.operation_output_policies WHERE operation=t.operation AND version=t.operation_version)<>5
 OR EXISTS(SELECT 1 FROM (VALUES ('defect_labels','application/octet-stream'),('defect_protection','application/octet-stream'),('defect_labels_png','image/png'),('defect_candidates','application/json'),('defect_propose_report','application/json')) expected(role,media)
 WHERE NOT EXISTS(SELECT 1 FROM photo.operation_output_policies policy WHERE policy.operation=t.operation AND policy.version=t.operation_version AND policy.role=expected.role AND policy.media_types=jsonb_build_array(expected.media) AND policy.max_count=1 AND policy.max_bytes>0)) THEN RETURN false;END IF;
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
ALTER FUNCTION photo.lock_semantic_target_defect_graph_budget(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_defect_graph_budget(uuid,uuid,jsonb) FROM PUBLIC;
CREATE FUNCTION photo.lock_semantic_target_defect_admission_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
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
 IF photo.lock_v27_target_defect_graph_scope(p_tenant,p_run,(registration->>'task_id')::uuid) IS NOT TRUE THEN RETURN false;END IF;
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
 IF photo.semantic_target_defect_registration_binding_state(p_tenant,p_run,'defects.propose',registration) IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_target_defect_graph_budget(p_tenant,p_run,jsonb_build_object('operation','defects.propose','actor_id',actor,'snapshot_id',w.input_snapshot_id,'registration',jsonb_build_object('task_id',n->'task_id')));
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_target_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
