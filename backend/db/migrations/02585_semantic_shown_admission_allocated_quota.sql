-- +goose Up
-- Repeated admission includes allocations from the same live run reservation.
-- Refunded allocations must not count twice; all caller/source checks remain.
-- +goose StatementBegin
DO $patch$
DECLARE item record;prior record;revised text;
 needle text:='q.bytes>= (t.execution_profile->''resource_estimate''->>''output_bytes'')::bigint';
 replacement text:='q.bytes::numeric+coalesce((SELECT sum(u.bytes)::numeric FROM photo.output_uploads u JOIN photo.tasks allocated ON allocated.tenant_id=u.tenant_id AND allocated.id=u.task_id WHERE u.tenant_id=q.tenant_id AND u.run_reservation_id=q.id AND NOT u.returned_to_run_reservation AND allocated.run_id=r.id),0)>= (t.execution_profile->''resource_estimate''->>''output_bytes'')::numeric';
BEGIN
 FOR item IN SELECT * FROM (VALUES
 (1,'photo.admit_semantic_shown_exposure(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'),
 (2,'photo.admit_semantic_shown_removal(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'),
 (3,'photo.admit_semantic_shown_background_cleanup(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'),
 (4,'photo.admit_semantic_shown_retouch(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)')) AS changes(ordinal,signature) ORDER BY ordinal LOOP
  SELECT p.oid,p.prosrc,pg_get_functiondef(p.oid) definition,p.proowner,p.proacl,p.proconfig,p.prosecdef,
   p.provolatile,p.prorettype,p.proargtypes,p.proargnames,p.proargmodes,p.proallargtypes,p.prokind,p.proretset
  INTO prior FROM pg_proc p WHERE p.oid=to_regprocedure(item.signature)
   AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND p.prokind='f' AND NOT p.proretset
   AND p.prorettype='boolean'::regtype AND p.pronargs=10
   AND p.proargtypes='2950 2950 2950 23 20 2950 2950 25 25 25'::oidvector
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   AND p.proconfig=ARRAY['search_path=pg_catalog, photo'];
  IF prior.oid IS NULL OR (length(prior.prosrc)-length(replace(prior.prosrc,needle,'')))/length(needle)<>1
   OR strpos(prior.prosrc,replacement)>0 THEN RAISE EXCEPTION 'SHOWN_ADMISSION_QUOTA_SOURCE_SHAPE: %',item.signature;END IF;
  IF EXISTS(SELECT 1 FROM aclexplode(coalesce(prior.proacl,acldefault('f',prior.proowner))) a
   WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(prior.proowner,'photo_worker'::regrole))
   OR NOT has_function_privilege('photo_worker',prior.oid,'EXECUTE')
   OR EXISTS(SELECT 1 FROM pg_roles role WHERE NOT role.rolsuper AND role.oid<>prior.proowner
    AND NOT pg_has_role(role.oid,prior.proowner,'USAGE') AND NOT pg_has_role(role.oid,'photo_worker','USAGE')
    AND has_function_privilege(role.oid,prior.oid,'EXECUTE'))
  THEN RAISE EXCEPTION 'SHOWN_ADMISSION_QUOTA_AUTHORITY_SHAPE: %',item.signature;END IF;
  revised:=replace(prior.prosrc,needle,replacement);
  EXECUTE replace(prior.definition,prior.prosrc,revised);
  IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=prior.oid AND p.prosrc=revised
   AND p.proowner=prior.proowner AND p.proacl IS NOT DISTINCT FROM prior.proacl AND p.proconfig IS NOT DISTINCT FROM prior.proconfig
   AND p.prosecdef=prior.prosecdef AND p.provolatile=prior.provolatile AND p.prorettype=prior.prorettype
   AND p.proargtypes=prior.proargtypes AND p.proargnames IS NOT DISTINCT FROM prior.proargnames
   AND p.proargmodes IS NOT DISTINCT FROM prior.proargmodes AND p.proallargtypes IS NOT DISTINCT FROM prior.proallargtypes
   AND p.prokind=prior.prokind AND p.proretset=prior.proretset)
  THEN RAISE EXCEPTION 'SHOWN_ADMISSION_QUOTA_AUTHORITY_CHANGED: %',item.signature;END IF;
 END LOOP;
END $patch$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
