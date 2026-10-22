-- +goose Up
-- All leased IO, including direct dispatch references, repeats exposure-preview
-- authority. The predicate is independent of execution_source_live (no cycle).
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_task_live(p_task uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;expected numeric;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 IF t.operation IS DISTINCT FROM 'semantic.preview_local_exposure' THEN RETURN true;END IF;
 IF t.state IS DISTINCT FROM 'running' OR t.cancel_requested OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation AND op.version=t.operation_version AND op.v2_enabled)
 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'preview' OR r.status IS DISTINCT FROM 'running'
 OR NOT r.graph_sealed OR r.cancel_requested
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=t.tenant_id AND status='active')
 OR photo.semantic_preapply_preview_closure_ready(t.id) IS DISTINCT FROM true
 OR photo.semantic_preapply_preview_run_current(t.tenant_id,r.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT intent.project_id INTO project FROM photo.semantic_intents intent JOIN photo.projects p
 ON p.tenant_id=intent.tenant_id AND p.id=intent.project_id AND p.archived_at IS NULL
 WHERE intent.tenant_id=t.tenant_id AND intent.id::text=r.config_snapshot->'semantic_preapply_preview'->'params'->>'intent_id';
 IF project IS NULL OR photo.v27_downstream_profile_ready(t.tenant_id,project,'semantic.local_exposure','1','cpu',r.v27_preapply_preview_profile) IS DISTINCT FROM true THEN RETURN false;END IF;
 expected:=(r.v27_preapply_preview_profile->'resource_estimate'->>'output_bytes')::numeric;
 RETURN coalesce(expected BETWEEN 1 AND 9007199254740991 AND EXISTS(
 SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
 AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL
 AND q.bytes::numeric+coalesce((SELECT sum(u.bytes)::numeric FROM photo.output_uploads u JOIN photo.tasks allocated
 ON allocated.tenant_id=u.tenant_id AND allocated.id=u.task_id WHERE u.tenant_id=q.tenant_id AND u.run_reservation_id=q.id AND NOT u.returned_to_run_reservation AND allocated.run_id=r.id),0)=expected
 AND q.expires_at>clock_timestamp()),false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_task_live(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_task_live(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
DO $patch$
DECLARE prior record;revised text;
 needle text:=' AND photo.semantic_shown_exposure_task_live(p_task) IS TRUE';
BEGIN
 SELECT p.oid,p.prosrc,pg_get_functiondef(p.oid) definition,p.proowner,p.proacl,p.proconfig,p.provolatile INTO prior
 FROM pg_proc p WHERE p.oid=to_regprocedure('photo.execution_source_live(uuid)')
 AND p.prosecdef AND p.proowner='photo_fn_owner'::regrole AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
 AND p.prokind='f' AND NOT p.proretset AND p.prorettype='boolean'::regtype AND p.proargtypes='2950'::oidvector
 AND p.proconfig=ARRAY['search_path=pg_catalog, photo'];
 IF prior.oid IS NULL OR (length(prior.prosrc)-length(replace(prior.prosrc,needle,'')))/length(needle)<>1
 OR strpos(prior.prosrc,'semantic_preapply_preview_task_live')>0 THEN RAISE EXCEPTION 'EXPOSURE_PREVIEW_LIVE_SOURCE_SHAPE';END IF;
 IF EXISTS(SELECT 1 FROM aclexplode(coalesce(prior.proacl,acldefault('f',prior.proowner))) a WHERE a.privilege_type='EXECUTE' AND a.grantee<>prior.proowner)
 OR EXISTS(SELECT 1 FROM pg_roles role WHERE NOT role.rolsuper AND role.oid<>prior.proowner
 AND NOT pg_has_role(role.oid,prior.proowner,'USAGE') AND has_function_privilege(role.oid,prior.oid,'EXECUTE'))
 THEN RAISE EXCEPTION 'EXPOSURE_PREVIEW_LIVE_HELPER_EXPOSED';END IF;
 revised:=replace(prior.prosrc,needle,E' AND photo.semantic_preapply_preview_task_live(p_task) IS TRUE\n'||needle);
 EXECUTE replace(prior.definition,prior.prosrc,revised);
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=prior.oid AND p.prosrc=revised AND p.proowner=prior.proowner
 AND p.proacl IS NOT DISTINCT FROM prior.proacl AND p.proconfig IS NOT DISTINCT FROM prior.proconfig AND p.prosecdef AND p.provolatile=prior.provolatile)
 THEN RAISE EXCEPTION 'EXPOSURE_PREVIEW_LIVE_AUTHORITY_CHANGED';END IF;
END $patch$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
