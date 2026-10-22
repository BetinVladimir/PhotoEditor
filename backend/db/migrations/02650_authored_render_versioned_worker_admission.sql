-- +goose Up
-- Additive worker version authority, not qualification issuance or activation.
-- No bindings are installed here. Historical compose/1 retains its old gates.
CREATE TABLE photo.authored_render_worker_installations (
 tenant_id uuid NOT NULL, worker_id uuid NOT NULL, operation text NOT NULL,
 operation_version text NOT NULL, project_id uuid NOT NULL, qualification_id uuid NOT NULL,
 qualification_pin jsonb NOT NULL, node_id text NOT NULL, db_principal name NOT NULL,
 certificate_sha256 text NOT NULL CHECK(certificate_sha256 ~ '^[0-9a-f]{64}$'),
 enabled boolean NOT NULL DEFAULT false,
 PRIMARY KEY(tenant_id,worker_id,operation),
 CHECK((operation='render.compose' AND operation_version='2') OR
       (operation IN('render.authored_segment','render.authored_geometry') AND operation_version='1')),
 FOREIGN KEY(tenant_id,worker_id) REFERENCES photo.worker_registrations(tenant_id,worker_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 FOREIGN KEY(tenant_id,qualification_id) REFERENCES photo.model_qualifications(tenant_id,id));
ALTER TABLE photo.authored_render_worker_installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.authored_render_worker_installations FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.authored_render_worker_installations TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.authored_render_worker_installations FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance,photo_auth,photo_publisher,photo_semantic_admission;
GRANT SELECT,INSERT,UPDATE ON photo.authored_render_worker_installations TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.authored_render_pair_evidence(p_tenant uuid,p_project uuid,p_operation text,p_version text,p_pin jsonb,p_qualification uuid)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
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
   AND (p_pin->>'calibration_sha')=ANY(p.calibration_shas))),false));
END $$;
ALTER FUNCTION photo.authored_render_pair_evidence(uuid,uuid,text,text,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authored_render_pair_evidence(uuid,uuid,text,text,jsonb,uuid) FROM PUBLIC;

CREATE FUNCTION photo.configure_authored_render_worker(p_tenant uuid,p_worker uuid,p_project uuid,p_operation text,p_version text,p_pin jsonb,p_qualification uuid,p_enable boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.worker_registrations%ROWTYPE;
BEGIN
 IF p_enable IS NULL THEN RETURN false;END IF;
 -- Same global order as existing root/worker administration; no task locks.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Disable exactly the existing immutable binding even after worker/report or
 -- policy revocation. No identity/pin/version can be changed through this path.
 IF p_enable IS FALSE THEN
  UPDATE photo.authored_render_worker_installations SET enabled=false
  WHERE tenant_id=p_tenant AND worker_id=p_worker AND project_id=p_project
   AND operation=p_operation AND operation_version=p_version
   AND qualification_id=p_qualification AND qualification_pin=p_pin;
  RETURN FOUND;
 END IF;
 IF photo.authored_render_pair_evidence(p_tenant,p_project,p_operation,p_version,p_pin,p_qualification) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker FOR UPDATE;
 IF NOT FOUND OR w.enabled IS NOT TRUE OR w.execution_pool IS DISTINCT FROM 'general'
 OR w.db_principal IS NULL OR w.node_id IS NULL OR w.certificate_sha256 IS NULL
 OR (w.resource_classes ? 'gpu') IS NOT TRUE OR (w.backends ? 'cpu') IS NOT TRUE
 OR ((w.qualified_operations->p_operation) ? 'cpu') IS NOT TRUE THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM pg_roles p WHERE p.rolname=w.db_principal AND p.rolcanlogin
  AND NOT p.rolsuper AND NOT p.rolbypassrls AND NOT p.rolcreaterole AND NOT p.rolreplication
  AND pg_has_role(p.oid,'photo_worker','MEMBER'))
 OR EXISTS(SELECT FROM pg_roles p WHERE (p.rolsuper OR p.rolbypassrls OR p.rolcreaterole OR p.rolreplication
  OR p.rolname IN('photo_fn_owner','photo_runtime_owner')) AND pg_has_role(w.db_principal,p.oid,'MEMBER'))
 OR has_schema_privilege(w.db_principal,'photo','CREATE') THEN RETURN false;END IF;
 -- A worker name cannot simultaneously advertise compose/1 and compose/2.
 -- This immutable version key also prevents reinterpreting an installed worker
 -- while older commands may still be queued. Replacement requires a new worker.
 IF EXISTS(SELECT FROM photo.authored_render_worker_installations i WHERE i.tenant_id=p_tenant AND i.worker_id=p_worker AND i.operation=p_operation
  AND (i.operation_version IS DISTINCT FROM p_version OR i.project_id IS DISTINCT FROM p_project
   OR i.qualification_id IS DISTINCT FROM p_qualification OR i.qualification_pin IS DISTINCT FROM p_pin
   OR i.db_principal IS DISTINCT FROM w.db_principal OR i.node_id IS DISTINCT FROM w.node_id
   OR i.certificate_sha256 IS DISTINCT FROM w.certificate_sha256)) THEN RETURN false;END IF;
 IF photo.authored_render_pair_evidence(p_tenant,p_project,p_operation,p_version,p_pin,p_qualification) IS NOT TRUE THEN RETURN false;END IF;
 INSERT INTO photo.authored_render_worker_installations VALUES(p_tenant,p_worker,p_operation,p_version,p_project,p_qualification,p_pin,w.node_id,w.db_principal,w.certificate_sha256,p_enable)
 ON CONFLICT(tenant_id,worker_id,operation) DO UPDATE SET enabled=excluded.enabled;
 RETURN true;
END $$;
ALTER FUNCTION photo.configure_authored_render_worker(uuid,uuid,uuid,text,text,jsonb,uuid,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.configure_authored_render_worker(uuid,uuid,uuid,text,text,jsonb,uuid,boolean) FROM PUBLIC;

-- Used identically by candidate inventory, reservation, actual targeted queue
-- selection and claim. This adds no source/recipe or provider authority.
CREATE FUNCTION photo.authored_render_worker_pair_ready(p_task uuid,p_worker uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
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
   AND photo.authored_render_pair_evidence(i.tenant_id,i.project_id,i.operation,i.operation_version,i.qualification_pin,i.qualification_id))
 ELSE true END,false) FROM photo.tasks t WHERE t.id=p_task
$$;
ALTER FUNCTION photo.authored_render_worker_pair_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authored_render_worker_pair_ready(uuid,uuid) FROM PUBLIC;

-- Qualification witness only. Future compute MUST additionally prove complete
-- authored instruction/source authority and physical typed closure; this JSON
-- does not grant acceptance, publication, adoption or completion.
CREATE FUNCTION photo.authored_render_worker_qualification(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
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
  ok:=photo.authored_render_final_profile_current(p_tenant,project,profile);
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
END $$;
ALTER FUNCTION photo.authored_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authored_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
-- Close inherited default EXECUTE grants as well as PUBLIC.
DO $$ DECLARE f record;a record;BEGIN
 FOR f IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo'
 AND p.proname IN('authored_render_pair_evidence','configure_authored_render_worker','authored_render_worker_pair_ready','authored_render_worker_qualification') LOOP
  FOR a IN SELECT grantee FROM aclexplode((SELECT coalesce(proacl,acldefault('f',proowner)) FROM pg_proc WHERE oid=f.oid))
   WHERE grantee<>0 AND grantee<>(SELECT proowner FROM pg_proc WHERE oid=f.oid) LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f.oid::regprocedure,pg_get_userbyid(a.grantee));
  END LOOP;
 END LOOP;
END $$;
GRANT EXECUTE ON FUNCTION photo.configure_authored_render_worker(uuid,uuid,uuid,text,text,jsonb,uuid,boolean) TO photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.authored_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd

-- Exact installed2649 source-CAS: preserve every earlier guard and metadata.
-- +goose StatementBegin
DO $patch$ DECLARE e jsonb;target_oid oid;before_meta jsonb;definition text;BEGIN
 FOR e IN SELECT value FROM jsonb_array_elements($spec$[{"signature":"photo.eligible_dispatch_nodes(uuid)","before_body_sha256":"c141806754963d19feb2690aa6e9cf050a5b285dd8cb9bbbfff6ec39eece773a","needle":"AND w.resource_classes ? t.resource_class","replacement":"AND photo.authored_render_worker_pair_ready(t.id,w.worker_id)\n AND w.resource_classes ? t.resource_class"},{"signature":"photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)","before_body_sha256":"d2fd12425316862f55ac99ddb4f472989e6ee74a08998ef56b7f27a8b04af8c8","needle":"AND w.db_principal IS NOT NULL AND w.resource_classes ? t.resource_class","replacement":"AND photo.authored_render_worker_pair_ready(t.id,w.worker_id)\n AND w.db_principal IS NOT NULL AND w.resource_classes ? t.resource_class"},{"signature":"photo.reserve_dispatch_before_retouch_preview(uuid,text,bigint,bigint,bigint,uuid,uuid,text)","before_body_sha256":"77461d74a4be4f06077bd8c414022b950da86aa2f132df784b60ba34cfc6bd62","needle":"AND w.resource_classes ? t.resource_class","replacement":"AND photo.authored_render_worker_pair_ready(t.id,w.worker_id)\n  AND w.resource_classes ? t.resource_class"},{"signature":"photo.claim_task(uuid,integer,uuid,jsonb)","before_body_sha256":"8d8958dc49a833af24ecabb8ac46642d12ca893e015533326a753a3cbaa7da23","needle":"IF claimed.outcome='claimed' THEN","replacement":"IF claimed.outcome='claimed' THEN\n  IF photo.authored_render_worker_pair_ready(p_task,p_worker) IS NOT TRUE THEN\n   RAISE EXCEPTION 'AUTHORED_RENDER_WORKER_VERSION_STALE' USING ERRCODE='40001';\n  END IF;"}]$spec$::jsonb) LOOP
  target_oid:=to_regprocedure(e->>'signature');
  IF target_oid IS NULL OR (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE pg_proc.oid=target_oid) IS DISTINCT FROM e->>'before_body_sha256' THEN
   RAISE EXCEPTION 'AUTHORED_RENDER_DISPATCH_PREIMAGE_CHANGED';END IF;
  SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE p.oid=target_oid;
  definition:=pg_get_functiondef(target_oid);
  IF (length(definition)-length(replace(definition,e->>'needle','')))/length(e->>'needle')<>1 THEN RAISE EXCEPTION 'AUTHORED_RENDER_DISPATCH_PATCH_COUNT';END IF;
  EXECUTE replace(definition,e->>'needle',e->>'replacement');
  IF (SELECT to_jsonb(p)-'prosrc' FROM pg_proc p WHERE p.oid=target_oid) IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_RENDER_DISPATCH_METADATA_CHANGED';END IF;
 END LOOP;
END $patch$;
-- +goose StatementEnd

-- The only DELETE consumer is the existing fenced tenant finisher's explicit
-- allowlist. Remove the FK-owning row before qualifications/projects/workers.
GRANT DELETE ON photo.authored_render_worker_installations TO photo_fn_owner;
-- +goose StatementBegin
DO $lifecycle$ DECLARE target oid;before_meta jsonb;definition text;BEGIN
 target:=to_regprocedure('photo.finish_tenant_deletion(uuid,uuid)');
 IF target IS NULL OR (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid=target) IS DISTINCT FROM '01eee5eb24f8f89cd94b9d64ff1fcfe656c1e26bc05fd4fee8d794f5ff64e4b7' THEN RAISE EXCEPTION 'AUTHORED_RENDER_PURGE_PREIMAGE_CHANGED';END IF;
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE p.oid=target;
 definition:=pg_get_functiondef(target);
 IF (length(definition)-length(replace(definition,'''native_render_installations''','')))/length('''native_render_installations''')<>1
 OR strpos(definition,'''authored_render_worker_installations''')<>0 THEN RAISE EXCEPTION 'AUTHORED_RENDER_PURGE_PATCH_COUNT';END IF;
 EXECUTE replace(definition,'''native_render_installations''','''authored_render_worker_installations'',''native_render_installations''');
 IF (SELECT to_jsonb(p)-'prosrc' FROM pg_proc p WHERE p.oid=target) IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_RENDER_PURGE_METADATA_CHANGED';END IF;
END $lifecycle$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
