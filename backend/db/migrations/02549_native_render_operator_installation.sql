-- +goose Up
-- Operator bindings authenticate exact native runtime/calibration installations.
-- They do not manufacture measured qualification or activate an operation.
CREATE TABLE photo.native_render_installations(
 tenant_id uuid NOT NULL,project_id uuid NOT NULL,operation text NOT NULL,
 qualification_id uuid NOT NULL,qualification_pin jsonb NOT NULL,
 icc_lock_sha text NOT NULL CHECK(icc_lock_sha ~ '^[0-9a-f]{64}$'),
 report_artifact_id uuid NOT NULL,report_sha text NOT NULL CHECK(report_sha ~ '^[0-9a-f]{64}$'),
 enabled boolean NOT NULL DEFAULT true,
 PRIMARY KEY(tenant_id,project_id,operation),
 CHECK(operation IN('editor.layers_precompose','render.compose')),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 FOREIGN KEY(tenant_id,qualification_id) REFERENCES photo.model_qualifications(tenant_id,id),
 FOREIGN KEY(tenant_id,report_artifact_id) REFERENCES photo.artifacts(tenant_id,id));
ALTER TABLE photo.native_render_installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.native_render_installations FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.native_render_installations TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.native_render_installations FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.native_render_installations TO photo_fn_owner;
CREATE TRIGGER ready_artifact_reference BEFORE INSERT OR UPDATE ON photo.native_render_installations
 FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference('report_artifact_id');
-- +goose StatementBegin
CREATE FUNCTION photo.native_render_installation_evidence(p_tenant uuid,p_project uuid,p_operation text,p_pin jsonb,p_icc text,p_qualification uuid,p_report uuid,p_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_operation IN('editor.layers_precompose','render.compose')
 AND jsonb_typeof(p_pin)='object' AND (SELECT count(*) FROM jsonb_object_keys(p_pin))=6
 AND p_pin-'model_lock_sha'-'runtime_lock_sha'-'backend'-'device_profile_sha'-'dataset_sha'-'calibration_sha'='{}'::jsonb
 AND NOT EXISTS(SELECT FROM jsonb_each(p_pin) e WHERE jsonb_typeof(e.value)<>'string')
 AND p_pin->>'model_lock_sha' ~ '^[0-9a-f]{64}$' AND p_pin->>'runtime_lock_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'device_profile_sha' ~ '^[0-9a-f]{64}$' AND p_pin->>'dataset_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'calibration_sha' ~ '^[0-9a-f]{64}$' AND p_icc ~ '^[0-9a-f]{64}$'
 AND ((p_operation='editor.layers_precompose' AND p_pin->>'backend'='cpu') OR (p_operation='render.compose' AND p_pin->>'backend' IN('cpu','cuda','mps')))
 AND EXISTS(SELECT FROM photo.tenants t JOIN photo.projects p ON p.tenant_id=t.id
 WHERE t.id=p_tenant AND t.status='active' AND p.id=p_project AND p.archived_at IS NULL)
 AND EXISTS(SELECT FROM photo.model_qualifications q JOIN photo.artifacts a ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id
 WHERE q.tenant_id=p_tenant AND q.project_id=p_project AND q.id=p_qualification
 AND q.operation=p_operation AND q.operation_version='1' AND q.status='passed' AND q.trusted_import
 AND q.model_lock_sha=p_pin->>'model_lock_sha' AND q.runtime_lock_sha=p_pin->>'runtime_lock_sha'
 AND q.backend=p_pin->>'backend' AND q.device_profile_sha=p_pin->>'device_profile_sha' AND q.dataset_sha=p_pin->>'dataset_sha'
 AND EXISTS(SELECT FROM photo.v2_qualification_policies policy WHERE policy.operation=q.operation AND policy.operation_version=q.operation_version
  AND policy.model_lock_sha=q.model_lock_sha AND policy.runtime_lock_sha=q.runtime_lock_sha AND policy.backend=q.backend
  AND policy.device_profile_sha=q.device_profile_sha AND policy.dataset_sha=q.dataset_sha
  AND policy.enabled AND policy.license_cleared AND policy.admission_enabled
  AND (p_pin->>'calibration_sha')=ANY(policy.calibration_shas))
 AND q.report_artifact_id=p_report AND q.report_sha=p_sha AND a.sha256=p_sha
 AND a.state='ready' AND a.media_type='application/json' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())),false)
$$;
ALTER FUNCTION photo.native_render_installation_evidence(uuid,uuid,text,jsonb,text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.native_render_installation_evidence(uuid,uuid,text,jsonb,text,uuid,uuid,text) FROM PUBLIC;
CREATE FUNCTION photo.configure_native_render_installation(p_tenant uuid,p_project uuid,p_operation text,p_pin jsonb,p_icc text,p_qualification uuid,p_report uuid,p_sha text,p_enable boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_enable IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.native_render_installations WHERE tenant_id=p_tenant AND project_id=p_project AND operation=p_operation FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_report FOR SHARE;
 PERFORM id FROM photo.model_qualifications WHERE tenant_id=p_tenant AND id=p_qualification FOR SHARE;
 IF photo.native_render_installation_evidence(p_tenant,p_project,p_operation,p_pin,p_icc,p_qualification,p_report,p_sha) IS DISTINCT FROM true THEN RETURN false;END IF;
 INSERT INTO photo.native_render_installations VALUES(p_tenant,p_project,p_operation,p_qualification,p_pin,p_icc,p_report,p_sha,p_enable)
 ON CONFLICT(tenant_id,project_id,operation) DO UPDATE SET qualification_id=excluded.qualification_id,
 qualification_pin=excluded.qualification_pin,icc_lock_sha=excluded.icc_lock_sha,report_artifact_id=excluded.report_artifact_id,report_sha=excluded.report_sha,enabled=excluded.enabled;
 RETURN true;
END $$;
ALTER FUNCTION photo.configure_native_render_installation(uuid,uuid,text,jsonb,text,uuid,uuid,text,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.configure_native_render_installation(uuid,uuid,text,jsonb,text,uuid,uuid,text,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.configure_native_render_installation(uuid,uuid,text,jsonb,text,uuid,uuid,text,boolean) TO photo_maintenance;
CREATE FUNCTION photo.native_render_installation_ready(p_tenant uuid,p_project uuid,p_worker uuid,p_operation text,p_pin jsonb,p_icc text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE installation photo.native_render_installations%ROWTYPE;role_name text:=current_setting('role',true);current_pin record;class_name text;
BEGIN
 IF role_name NOT IN('photo_worker','photo_scheduler') OR role_name IS NULL
 OR NOT pg_has_role(session_user,role_name,'MEMBER') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM pg_roles r WHERE r.rolname=session_user AND NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreaterole AND NOT r.rolreplication)
 OR EXISTS(SELECT FROM pg_roles privileged WHERE
  (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreaterole OR privileged.rolreplication
   OR privileged.rolname IN('photo_fn_owner','photo_runtime_owner')
   OR EXISTS(SELECT FROM pg_namespace n WHERE n.nspname='photo' AND n.nspowner=privileged.oid)
   OR EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo' AND c.relowner=privileged.oid)
   OR EXISTS(SELECT FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.prosecdef AND p.proowner=privileged.oid))
  AND pg_has_role(session_user,privileged.oid,'MEMBER'))
 OR has_schema_privilege(session_user,'photo','CREATE') THEN RETURN false;END IF;
 SELECT * INTO installation FROM photo.native_render_installations WHERE tenant_id=p_tenant AND project_id=p_project AND operation=p_operation AND enabled;
 IF NOT FOUND OR installation.qualification_pin IS DISTINCT FROM p_pin OR installation.icc_lock_sha IS DISTINCT FROM p_icc
 OR photo.native_render_installation_evidence(p_tenant,p_project,p_operation,p_pin,p_icc,installation.qualification_id,installation.report_artifact_id,installation.report_sha) IS DISTINCT FROM true THEN RETURN false;END IF;
 class_name:=CASE WHEN p_operation='editor.layers_precompose' THEN 'cpu' ELSE 'gpu' END;
 IF role_name='photo_worker' THEN
  IF p_worker IS NULL OR NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker
   AND w.db_principal=session_user AND w.enabled AND w.execution_pool='general'
   AND w.resource_classes ? class_name AND w.backends ? (p_pin->>'backend')
   AND (w.qualified_operations->p_operation) ? (p_pin->>'backend')) THEN RETURN false;END IF;
 ELSIF p_worker IS NOT NULL THEN RETURN false;END IF;
 IF p_operation='editor.layers_precompose' THEN
  RETURN photo.editor_internal_dispatch_installed(p_operation) IS TRUE;
 END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT * INTO current_pin FROM photo.resolve_v26_render_qualification(p_tenant,p_project);
 RETURN FOUND AND current_pin.model_lock_sha=p_pin->>'model_lock_sha' AND current_pin.runtime_lock_sha=p_pin->>'runtime_lock_sha'
 AND current_pin.backend=p_pin->>'backend' AND current_pin.device_profile_sha=p_pin->>'device_profile_sha' AND current_pin.dataset_sha=p_pin->>'dataset_sha';
END $$;
ALTER FUNCTION photo.native_render_installation_ready(uuid,uuid,uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.native_render_installation_ready(uuid,uuid,uuid,text,jsonb,text) FROM PUBLIC;
-- Remove default explicit ACLs, not just PUBLIC, from private/new authority functions.
DO $$ DECLARE entry record;BEGIN
 FOR entry IN SELECT p.oid,a.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE p.oid IN('photo.native_render_installation_evidence(uuid,uuid,text,jsonb,text,uuid,uuid,text)'::regprocedure,
 'photo.configure_native_render_installation(uuid,uuid,text,jsonb,text,uuid,uuid,text,boolean)'::regprocedure,
 'photo.native_render_installation_ready(uuid,uuid,uuid,text,jsonb,text)'::regprocedure)
 AND a.grantee<>0 AND a.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',entry.oid::regprocedure,pg_get_userbyid(entry.grantee));END LOOP;
END $$;
GRANT EXECUTE ON FUNCTION photo.configure_native_render_installation(uuid,uuid,text,jsonb,text,uuid,uuid,text,boolean) TO photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.native_render_installation_ready(uuid,uuid,uuid,text,jsonb,text) TO photo_worker,photo_scheduler;
CREATE FUNCTION photo.editor_precompose_installation_pin(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE installation photo.native_render_installations%ROWTYPE;
BEGIN
 IF photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO installation FROM photo.native_render_installations WHERE tenant_id=p_tenant AND project_id=p_project AND operation='editor.layers_precompose' AND enabled FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=installation.report_artifact_id FOR SHARE;
 PERFORM id FROM photo.model_qualifications WHERE tenant_id=p_tenant AND id=installation.qualification_id FOR SHARE;
 PERFORM id FROM photo.v2_qualification_policies WHERE operation=installation.operation AND operation_version='1'
 AND model_lock_sha=installation.qualification_pin->>'model_lock_sha' AND runtime_lock_sha=installation.qualification_pin->>'runtime_lock_sha'
 AND backend=installation.qualification_pin->>'backend' AND device_profile_sha=installation.qualification_pin->>'device_profile_sha'
 AND dataset_sha=installation.qualification_pin->>'dataset_sha' ORDER BY id FOR SHARE;
 IF photo.native_render_installation_evidence(p_tenant,p_project,installation.operation,installation.qualification_pin,installation.icc_lock_sha,installation.qualification_id,installation.report_artifact_id,installation.report_sha) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('qualification_pin',installation.qualification_pin,'icc_lock_sha',installation.icc_lock_sha);
END $$;
ALTER FUNCTION photo.editor_precompose_installation_pin(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_precompose_installation_pin(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.editor_precompose_installation_pin(uuid,uuid,uuid,uuid) TO photo_api;
-- Preserve the complete existing origin/attempt/input/lease gate and extend it
-- with the server-frozen CPU calibration binding, rather than latest budgets.
DO $$ DECLARE definition text;needle constant text:=' RETURN true;';replacement text;BEGIN
 definition:=pg_get_functiondef('photo.editor_layers_precompose_worker_origin_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 OR strpos(definition,'native_qualification_pin')>0 THEN RAISE EXCEPTION 'NATIVE_PRECOMPOSE_WORKER_SOURCE_SHAPE';END IF;
 replacement:=$patch$
 IF t.execution_profile IS DISTINCT FROM r.config_snapshot#>'{editor_precompose,execution_profile}'
 OR photo.native_render_installation_ready(p_tenant,o.project_id,p_worker,'editor.layers_precompose',t.execution_profile->'native_qualification_pin',t.execution_profile->>'icc_lock_sha') IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN true;$patch$;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- Only a scheduler holding the current Apply expansion lease may settle an
-- accepted legacy prestage with no executable frozen calibrated profile.
CREATE FUNCTION photo.fail_editor_precompose_profile(p_run uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;origin photo.editor_effect_builds%ROWTYPE;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.phase<>'editor_precompose' OR b.lease_token IS DISTINCT FROM p_token
 OR b.lease_until IS NULL OR b.lease_until<=clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=b.tenant_id AND id=p_run FOR UPDATE;
 SELECT * INTO origin FROM photo.editor_effect_builds WHERE tenant_id=b.tenant_id AND run_id=p_run FOR UPDATE;
 IF origin.run_id IS NULL OR origin.state<>'building' OR r.id IS NULL OR r.kind<>'apply'
 OR r.cancel_requested OR r.status NOT IN('queued','running')
 OR r.config_snapshot->'editor_precompose' IS NULL
 OR (jsonb_typeof(r.config_snapshot#>'{editor_precompose,execution_profile}')='object'
  AND CASE WHEN jsonb_typeof(r.config_snapshot#>'{editor_precompose,execution_profile,native_qualification_pin}')='object' THEN
   (SELECT count(*) FROM jsonb_object_keys(r.config_snapshot#>'{editor_precompose,execution_profile,native_qualification_pin}'))=6
   AND r.config_snapshot#>>'{editor_precompose,execution_profile,native_qualification_pin,backend}'='cpu'
   AND r.config_snapshot#>>'{editor_precompose,execution_profile,native_qualification_pin,model_lock_sha}' ~ '^[0-9a-f]{64}$'
   AND r.config_snapshot#>>'{editor_precompose,execution_profile,native_qualification_pin,runtime_lock_sha}' ~ '^[0-9a-f]{64}$'
   AND r.config_snapshot#>>'{editor_precompose,execution_profile,native_qualification_pin,device_profile_sha}' ~ '^[0-9a-f]{64}$'
   AND r.config_snapshot#>>'{editor_precompose,execution_profile,native_qualification_pin,dataset_sha}' ~ '^[0-9a-f]{64}$'
   AND r.config_snapshot#>>'{editor_precompose,execution_profile,native_qualification_pin,calibration_sha}' ~ '^[0-9a-f]{64}$'
   ELSE false END)
 OR b.lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.tasks WHERE tenant_id=b.tenant_id AND run_id=p_run AND state NOT IN('succeeded','failed','cancelled','skipped')) THEN
  RETURN photo.fail_apply_build(p_run,p_token,'EDITOR_PRECOMPOSE_PROFILE_UNAVAILABLE');
 END IF;
 -- A crash may have materialized a legacy task before advancing the phase.
 -- Keep its quota until the ordinary cancellation engine settles every task.
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.apply_builds SET last_error='EDITOR_PRECOMPOSE_PROFILE_UNAVAILABLE',updated_at=clock_timestamp() WHERE tenant_id=b.tenant_id AND run_id=p_run;
 UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1 WHERE tenant_id=b.tenant_id AND id=p_run;
 RETURN true;
END $$;
ALTER FUNCTION photo.fail_editor_precompose_profile(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_editor_precompose_profile(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.fail_editor_precompose_profile(uuid,uuid) TO photo_scheduler;
DO $$ DECLARE entry record;BEGIN
 FOR entry IN SELECT p.oid,a.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE p.oid IN('photo.editor_precompose_installation_pin(uuid,uuid,uuid,uuid)'::regprocedure,'photo.fail_editor_precompose_profile(uuid,uuid)'::regprocedure)
 AND a.grantee<>0 AND a.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',entry.oid::regprocedure,pg_get_userbyid(entry.grantee));END LOOP;
END $$;
GRANT EXECUTE ON FUNCTION photo.editor_precompose_installation_pin(uuid,uuid,uuid,uuid) TO photo_api;
GRANT EXECUTE ON FUNCTION photo.fail_editor_precompose_profile(uuid,uuid) TO photo_scheduler;
-- Preserve existing GC predicate OID and all current roots.
DO $$ DECLARE body text;BEGIN
 SELECT prosrc INTO body FROM pg_proc WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]' OR strpos(body,'photo.native_render_installations')>0 THEN RAISE EXCEPTION 'NATIVE_RENDER_GC_SOURCE_SHAPE';END IF;
 body:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',E'\\1 EXISTS(SELECT FROM photo.native_render_installations n WHERE n.tenant_id=p_tenant AND n.report_artifact_id=p_artifact) OR ','i');
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',body);
END $$;
DO $$ DECLARE definition text;anchor constant text:='tables_to_purge constant text[]:=ARRAY[';BEGIN
 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 OR strpos(definition,'''native_render_installations''')>0 THEN RAISE EXCEPTION 'NATIVE_RENDER_PURGE_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,anchor,anchor||'''native_render_installations'',');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
