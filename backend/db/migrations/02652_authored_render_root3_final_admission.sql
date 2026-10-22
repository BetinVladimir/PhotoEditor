-- +goose Up
-- Requires the exact frozen02651 predecessor; no new qualification or binding.
-- +goose StatementBegin
DO $root3_lock_acl$ DECLARE before_acl aclitem[]; BEGIN
 SELECT relacl INTO before_acl FROM pg_class WHERE oid='photo.authored_final_generation_evidence'::regclass;
 IF before_acl IS DISTINCT FROM ARRAY['authored_goose_owner=arwdDxtm/authored_goose_owner','photo_fn_owner=ard/authored_goose_owner']::aclitem[]
 OR EXISTS(SELECT FROM pg_attribute WHERE attrelid='photo.authored_final_generation_evidence'::regclass AND attnum>0 AND NOT attisdropped AND attacl IS NOT NULL)
 THEN RAISE EXCEPTION 'AUTHORED_ROOT3_LOCK_ACL_PREIMAGE_CHANGED'; END IF;
 GRANT UPDATE(artifact_id) ON photo.authored_final_generation_evidence TO photo_fn_owner;
 IF (SELECT relacl FROM pg_class WHERE oid='photo.authored_final_generation_evidence'::regclass) IS DISTINCT FROM before_acl
 OR EXISTS(SELECT FROM pg_attribute a WHERE a.attrelid='photo.authored_final_generation_evidence'::regclass AND a.attnum>0 AND NOT a.attisdropped
  AND has_column_privilege('photo_fn_owner',a.attrelid,a.attnum,'UPDATE') IS DISTINCT FROM (a.attname='artifact_id'))
 OR (SELECT count(*) FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
  WHERE a.attrelid='photo.authored_final_generation_evidence'::regclass AND a.attnum>0 AND NOT a.attisdropped)<>1
 OR NOT EXISTS(SELECT FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
  WHERE a.attrelid='photo.authored_final_generation_evidence'::regclass AND a.attname='artifact_id'
  AND x.grantee='photo_fn_owner'::regrole AND x.privilege_type='UPDATE' AND NOT x.is_grantable)
 THEN RAISE EXCEPTION 'AUTHORED_ROOT3_LOCK_ACL_POSTCONDITION'; END IF;
END $root3_lock_acl$;
DO $root3_guard_0$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure) IS DISTINCT FROM 'a8cad153d1aa25f6f1c8250b4c4d73a1df0ac637e5a695e2b2cfa60e2203e5a9' THEN RAISE EXCEPTION 'AUTHORED_ROOT3_PREIMAGE_CHANGED';END IF;
 EXECUTE $root3_definition_0$CREATE OR REPLACE FUNCTION photo.authored_render_worker_qualification(p_tenant uuid, p_run uuid, p_task uuid, p_attempt integer, p_fence bigint, p_worker uuid, p_params uuid, p_sha text, p_json text, p_digest text)
 RETURNS jsonb
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;profile jsonb;ok boolean;
BEGIN
 IF p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_json IS NULL
 OR octet_length(p_json) NOT BETWEEN 2 AND 33554432 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
 OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF t.id IS NULL OR r.id IS NULL OR t.state IS DISTINCT FROM 'running' OR t.cancel_requested IS DISTINCT FROM false
 OR r.status IS DISTINCT FROM 'running' OR r.cancel_requested IS DISTINCT FROM false OR r.graph_sealed IS DISTINCT FROM true
 OR t.worker_id IS DISTINCT FROM p_worker OR t.dispatch_worker_id IS DISTINCT FROM p_worker
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp() OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
 OR NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled)
 OR photo.authored_render_worker_pair_ready(p_task,p_worker) IS NOT TRUE
 OR NOT EXISTS(SELECT FROM photo.resource_slots s JOIN photo.worker_registrations w ON w.tenant_id=s.tenant_id AND w.worker_id=p_worker
  JOIN photo.resource_nodes n ON n.node_id=w.node_id WHERE s.tenant_id=p_tenant AND s.task_id=p_task AND s.node_id=w.node_id
  AND s.fencing_token=p_fence AND s.resource_class='gpu' AND s.lease_until>clock_timestamp()
  AND n.enabled AND photo.capacity_policy_worker_fresh(w) AND photo.capacity_policy_node_fresh(n))
 OR NOT EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=p_params AND i.sha256=p_sha AND i.contract='Params')
 OR photo.v2_input_closure_ready(p_task) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT w.project_id INTO project FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.renderer_version='v2.2';
 IF project IS NULL THEN RETURN NULL;END IF;
 profile:=t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha';
 CASE WHEN t.operation='render.compose' AND t.operation_version='2' THEN
  ok:=photo.authored_render_final_profile_dispatch_current(p_tenant,project,profile);
 WHEN t.operation='render.authored_segment' AND t.operation_version='1' THEN
  ok:=photo.authored_render_segment_profile_current(p_tenant,project,profile);
 WHEN t.operation='render.authored_geometry' AND t.operation_version='1' THEN
  ok:=photo.authored_render_geometry_profile_current(p_tenant,project,profile);
 ELSE RETURN NULL;END CASE;
 IF ok IS NOT TRUE THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'operation',t.operation,'operation_version',t.operation_version,
  'tenant_id',p_tenant,'run_id',p_run,'task_id',p_task,'worker_id',p_worker,'attempt',p_attempt,'fence',p_fence,
  'params',jsonb_build_object('artifact_id',p_params,'sha256',p_sha),'input_digest',p_digest,
  'qualification_pin',t.execution_profile->'native_qualification_pin');
END $function$
$root3_definition_0$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_ROOT3_METADATA_CHANGED';END IF;
END $root3_guard_0$;
DO $root3_guard_1$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_pair_evidence(uuid,uuid,text,text,jsonb,uuid)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_pair_evidence(uuid,uuid,text,text,jsonb,uuid)'::regprocedure) IS DISTINCT FROM 'f40adc884f8b396f46b5cc3f9c8b147eae19093a5912b13a2b76cf7a48769fcf' THEN RAISE EXCEPTION 'AUTHORED_ROOT3_PREIMAGE_CHANGED';END IF;
 EXECUTE $root3_definition_1$CREATE OR REPLACE FUNCTION photo.authored_render_pair_evidence(p_tenant uuid, p_project uuid, p_operation text, p_version text, p_pin jsonb, p_qualification uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
BEGIN
 IF jsonb_typeof(p_pin) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 RETURN (SELECT coalesce(
 ((p_operation='render.compose' AND p_version='2') OR
  (p_operation IN('render.authored_segment','render.authored_geometry') AND p_version='1'))
 AND jsonb_typeof(p_pin)='object'
 AND (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_pin) key)=
  ARRAY['backend','calibration_sha','dataset_sha','device_profile_sha','model_lock_sha','runtime_lock_sha']::text[]
 AND NOT EXISTS(SELECT FROM jsonb_each(p_pin) f WHERE jsonb_typeof(f.value)<>'string')
 AND p_pin->>'backend'='cpu'
 AND p_pin->>'calibration_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'model_lock_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'runtime_lock_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'device_profile_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'dataset_sha' ~ '^[0-9a-f]{64}$'
 AND EXISTS(SELECT FROM photo.tenants t JOIN photo.projects p ON p.tenant_id=t.id
  WHERE t.id=p_tenant AND t.status='active' AND p.id=p_project AND p.archived_at IS NULL)
 AND EXISTS(SELECT FROM photo.operation_registry o WHERE o.operation=p_operation AND o.version=p_version
  AND o.v2_enabled AND o.resource_class='gpu' AND o.backends ? 'cpu')
 AND EXISTS(SELECT FROM photo.model_qualifications q
  JOIN photo.artifacts a ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id AND a.sha256=q.report_sha
  WHERE q.tenant_id=p_tenant AND q.project_id=p_project AND q.id=p_qualification
  AND q.operation=p_operation AND q.operation_version=p_version AND q.trusted_import AND q.status='passed'
  AND q.model_lock_sha=p_pin->>'model_lock_sha' AND q.runtime_lock_sha=p_pin->>'runtime_lock_sha'
  AND q.backend=p_pin->>'backend' AND q.device_profile_sha=p_pin->>'device_profile_sha' AND q.dataset_sha=p_pin->>'dataset_sha'
  AND a.state='ready' AND a.media_type='application/json' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND EXISTS(SELECT FROM photo.v2_qualification_policies p WHERE p.operation=q.operation AND p.operation_version=q.operation_version
   AND p.model_lock_sha=q.model_lock_sha AND p.runtime_lock_sha=q.runtime_lock_sha AND p.backend=q.backend
   AND p.device_profile_sha=q.device_profile_sha AND p.dataset_sha=q.dataset_sha
   AND p.enabled AND p.admission_enabled AND p.license_cleared
   AND ((p_operation='render.authored_geometry' AND p_version='1'
    AND p.required_checks @> ARRAY['authored_geometry_schema_338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b','authored_geometry_calibration']::text[]
    AND p.evidence_shas @> ARRAY['338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b',p_pin->>'calibration_sha']::text[])
   OR ((p_operation<>'render.authored_geometry' AND (p_pin->>'calibration_sha')=ANY(p.calibration_shas)) OR (p_operation='render.compose' AND p_version='2'
    AND p.required_checks @> ARRAY['authored_final_schema_338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b','authored_final_calibration']::text[]
    AND p.evidence_shas @> ARRAY['338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b',p_pin->>'calibration_sha']::text[]
    AND EXISTS(SELECT FROM photo.authored_final_generation_evidence g JOIN photo.artifacts ga ON ga.tenant_id=g.tenant_id AND ga.id=g.artifact_id AND ga.sha256=g.sha256
     WHERE g.tenant_id=p_tenant AND g.project_id=p_project
     AND g.evidence_jcs::jsonb->>'contracts_sha'='338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b'
     AND g.evidence_jcs::jsonb->>'operation'='render.compose' AND g.evidence_jcs::jsonb->>'operation_version'='2'
     AND g.evidence_jcs::jsonb->'qualification'=p_pin-'calibration_sha'
     AND g.evidence_jcs::jsonb->>'calibration_sha256'=p_pin->>'calibration_sha'
     AND g.evidence_jcs::jsonb#>>'{qualification_report,artifact_id}'=q.report_artifact_id::text
     AND g.evidence_jcs::jsonb#>>'{qualification_report,sha256}'=q.report_sha
     AND encode(sha256(convert_to(g.report_jcs,'UTF8')),'hex')=q.report_sha
     AND ga.bytes=octet_length(g.evidence_jcs) AND ga.media_type='application/json' AND ga.state='ready' AND ga.verified_at IS NOT NULL
     AND ga.object_version IS NOT NULL AND ga.object_version NOT IN('','null') AND (ga.expires_at IS NULL OR ga.expires_at>clock_timestamp()))))))),false));
END $function$
$root3_definition_1$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_pair_evidence(uuid,uuid,text,text,jsonb,uuid)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_ROOT3_METADATA_CHANGED';END IF;
END $root3_guard_1$;
DO $root3_guard_2$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_worker_pair_ready(uuid,uuid)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_worker_pair_ready(uuid,uuid)'::regprocedure) IS DISTINCT FROM '1052deac52a523fdb5ccf60b816da619595c77f97f7e1914793523bacdbe48d9' THEN RAISE EXCEPTION 'AUTHORED_ROOT3_PREIMAGE_CHANGED';END IF;
 EXECUTE $root3_definition_2$CREATE OR REPLACE FUNCTION photo.authored_render_worker_pair_ready(p_task uuid, p_worker uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
 SELECT coalesce(CASE
 WHEN t.operation='render.compose' AND t.operation_version='1' THEN
  NOT EXISTS(SELECT FROM photo.authored_render_worker_installations i WHERE i.tenant_id=t.tenant_id AND i.worker_id=p_worker AND i.operation=t.operation)
 WHEN t.operation IN('render.compose','render.authored_segment','render.authored_geometry') THEN
  EXISTS(SELECT FROM photo.authored_render_worker_installations i
   JOIN photo.worker_registrations w ON w.tenant_id=i.tenant_id AND w.worker_id=i.worker_id
   WHERE i.tenant_id=t.tenant_id AND i.worker_id=p_worker AND i.operation=t.operation AND i.operation_version=t.operation_version AND i.enabled
   AND w.enabled AND w.execution_pool='general' AND w.lock_id=i.qualification_pin->>'model_lock_sha'
   AND w.resource_classes ? 'gpu' AND w.backends ? 'cpu' AND (w.qualified_operations->t.operation) ? 'cpu'
   AND i.node_id=w.node_id AND i.db_principal=w.db_principal AND i.certificate_sha256=w.certificate_sha256
   AND t.execution_profile->>'operation'=t.operation AND t.execution_profile->>'operation_version'=t.operation_version
   AND t.execution_profile->>'backend'='cpu' AND t.execution_profile->>'renderer_version'='v2.2' AND t.resource_class='gpu'
   AND t.execution_profile->'native_qualification_pin'=i.qualification_pin
   AND t.execution_profile->>'render_runtime_sha'=i.qualification_pin->>'runtime_lock_sha'
   AND t.execution_profile->>'model_lock_sha'=i.qualification_pin->>'model_lock_sha'
   AND photo.authored_render_pair_evidence(i.tenant_id,i.project_id,i.operation,i.operation_version,i.qualification_pin,i.qualification_id)
   AND (CASE WHEN t.operation='render.compose' AND t.operation_version='2' AND t.execution_profile->>'contracts_sha'='338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b' THEN EXISTS(
    SELECT FROM photo.authored_final_generation_evidence g JOIN photo.model_qualifications selected ON selected.tenant_id=g.tenant_id AND selected.id=i.qualification_id
    WHERE g.tenant_id=i.tenant_id AND g.project_id=i.project_id
    AND g.artifact_id=CASE WHEN t.execution_profile#>>'{generation_evidence,artifact_id}' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (t.execution_profile#>>'{generation_evidence,artifact_id}')::uuid ELSE NULL END AND g.sha256=t.execution_profile#>>'{generation_evidence,sha256}'
    AND selected.report_artifact_id::text=g.evidence_jcs::jsonb#>>'{qualification_report,artifact_id}'
    AND selected.report_sha=g.evidence_jcs::jsonb#>>'{qualification_report,sha256}'
    AND selected.operation=t.operation AND selected.operation_version=t.operation_version) ELSE true END))
 ELSE true END,false) FROM photo.tasks t WHERE t.id=p_task
$function$
$root3_definition_2$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_worker_pair_ready(uuid,uuid)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_ROOT3_METADATA_CHANGED';END IF;
END $root3_guard_2$;
DO $root3_guard_3$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.v2_input_closure_ready(uuid)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.v2_input_closure_ready(uuid)'::regprocedure) IS DISTINCT FROM '6c7777a3ee81b75ca508c64767cbdfd9e2ddee6bf70127bed57702cc102a272a' THEN RAISE EXCEPTION 'AUTHORED_ROOT3_PREIMAGE_CHANGED';END IF;
 EXECUTE $root3_definition_3$CREATE OR REPLACE FUNCTION photo.v2_input_closure_ready(p_task uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE t photo.tasks%ROWTYPE; manifest jsonb; ref jsonb; direct_count integer; entry_count integer; node jsonb; binding jsonb; config jsonb;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND OR jsonb_typeof(t.inputs) IS DISTINCT FROM 'array'
  OR (jsonb_typeof(t.inputs)='array' AND jsonb_array_length(t.inputs) NOT BETWEEN 1 AND 128)
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR (coalesce(t.execution_profile->>'contracts_sha','') NOT IN (
   '6c52d2a8828f7370520064925c29bb4a043be05d34934db612e7b1fb9016fad2',
   '341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815',
   '266da073ca9bf6863ac9f9a5f54b00f1ee702a415fd91fa735afea648428e685',
   '4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4') AND NOT (t.operation IN('semantic.transfer','v2.semantic.assemble') AND t.operation_version='1' AND coalesce(t.execution_profile->>'contracts_sha','')='dd261b1a9a7bb7d4775155dc25d6e672f068e6abb5d072bc2ddc5de6279ccb87') AND NOT ((t.operation='render.compose' AND t.operation_version='2' OR t.operation='render.authored_segment' AND t.operation_version='1') AND coalesce(t.execution_profile->>'contracts_sha','')='55c5e551578614890def6135458f535fa71d1d1725a6525d490588b3f29a4772' OR t.operation='render.authored_geometry' AND t.operation_version='1' AND coalesce(t.execution_profile->>'contracts_sha','')='338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b') AND NOT (t.operation='render.compose' AND t.operation_version='2' AND coalesce(t.execution_profile->>'contracts_sha','')='338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b' AND photo.authored_render_final_generation_inputs_current(t.tenant_id,t.id,t.execution_profile) IS TRUE)) THEN
  RETURN false;
 END IF;
 IF coalesce(t.execution_profile->>'v2_entry_count','') !~ '^[1-9][0-9]{0,6}$'
  OR coalesce(t.execution_profile->>'v2_entries_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 entry_count:=(t.execution_profile->>'v2_entry_count')::integer;
 IF entry_count>1000000 THEN RETURN false; END IF;
 manifest:=t.execution_profile->'v2_input_manifest';
 IF jsonb_typeof(manifest) IS DISTINCT FROM 'object' OR (manifest-'artifact_id'-'sha256')<>'{}'::jsonb
  OR manifest->>'artifact_id' IS NULL OR manifest->>'sha256' IS NULL
  OR manifest->>'sha256' !~ '^[0-9a-f]{64}$' THEN
  RETURN false;
 END IF;
 SELECT count(*) INTO direct_count FROM jsonb_array_elements(t.inputs) AS x(ref)
  WHERE x.ref=manifest;
 IF direct_count<>1 THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.kind='v2'
   AND w.renderer_version='v2.2') THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract='InputClosureManifest' AND i.artifact_id::text=manifest->>'artifact_id'
     AND i.sha256=manifest->>'sha256')<>1 THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract='InputClosureChunk')<>(entry_count+511)/512 THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract NOT IN ('InputClosureManifest','InputClosureChunk'))<>entry_count THEN
  RETURN false;
 END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(ref)<>'object' OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb
   OR ref->>'artifact_id' IS NULL OR ref->>'sha256' IS NULL
   OR ref->>'sha256' !~ '^[0-9a-f]{64}$' THEN
   RETURN false;
  END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.artifact_id::text=ref->>'artifact_id' AND i.sha256=ref->>'sha256') THEN
   RETURN false;
  END IF;
 END LOOP;
 IF t.operation='render.compose' AND NOT (t.operation_version='2' AND coalesce(t.execution_profile->>'contracts_sha','') IN('55c5e551578614890def6135458f535fa71d1d1725a6525d490588b3f29a4772','338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b')) THEN
  IF t.operation_version<>'1' OR entry_count NOT BETWEEN 2 AND 4096
   OR jsonb_array_length(t.inputs)<>2
   OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM '6c52d2a8828f7370520064925c29bb4a043be05d34934db612e7b1fb9016fad2' THEN RETURN false;END IF;
  SELECT config_snapshot INTO config FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
  IF jsonb_typeof(config->'v26_render'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT value INTO node FROM jsonb_array_elements(config->'v26_render'->'nodes') WHERE value->>'task_id'=t.id::text;
  IF node IS NULL OR t.params_artifact_id::text IS DISTINCT FROM node->'params'->>'artifact_id'
   OR t.inputs->1 IS DISTINCT FROM node->'params' THEN RETURN false;END IF;
  binding:=(node->>'params_jcs')::jsonb->'scalars'->'source';
  IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id AND contract='FrozenRenderRecipe')<>1
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.contract='FrozenRenderRecipe' AND i.artifact_id=t.params_artifact_id AND i.sha256=node->'params'->>'sha256')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.contract='WorkingImage' AND i.artifact_id::text=binding->'source'->>'artifact_id'
    AND i.sha256=binding->'source'->>'sha256') THEN RETURN false;END IF;
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $function$
$root3_definition_3$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.v2_input_closure_ready(uuid)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_ROOT3_METADATA_CHANGED';END IF;
END $root3_guard_3$;
-- +goose StatementEnd
-- +goose Down
-- Forward only.
