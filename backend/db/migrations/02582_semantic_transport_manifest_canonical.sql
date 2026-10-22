-- +goose Up
-- Match the RFC 8785 manifest emitted by v2input.BuildInputClosure. Only the
-- reconstruction's key order changes; input, authority and digest checks remain.
-- +goose StatementBegin
DO $patch$
DECLARE item record; prior record; revised text;
 needle text := $old$,"entry_count":'||n::text||',"entries_sha":"'||(p_transport->>'entries_sha')||'"$old$;
 replacement text := $new$,"entries_sha":"'||(p_transport->>'entries_sha')||'","entry_count":'||n::text||'$new$;
BEGIN
 FOR item IN SELECT * FROM (VALUES
 (1,'photo.semantic_preapply_preview_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)'),
 (2,'photo.semantic_shown_exposure_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)'),
 (3,'photo.semantic_removal_preview_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)'),
 (4,'photo.semantic_shown_removal_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)'),
 (5,'photo.semantic_background_cleanup_preview_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)'),
 (6,'photo.semantic_shown_background_cleanup_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)'),
 (7,'photo.semantic_retouch_preview_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)'),
 (8,'photo.semantic_shown_retouch_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)')) AS changes(ordinal,signature) ORDER BY ordinal LOOP
  SELECT p.oid,p.prosrc,pg_get_functiondef(p.oid) definition,p.proowner,p.proacl,p.proconfig,
   p.prosecdef,p.provolatile,p.prorettype,p.proargtypes,p.prokind,p.proretset
  INTO prior FROM pg_proc p WHERE p.oid=to_regprocedure(item.signature)
   AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND p.provolatile='i'
   AND p.prokind='f' AND NOT p.proretset AND p.prorettype='boolean'::regtype
   AND p.pronargs=5 AND p.proargtypes='2950 3802 3802 3802 3802'::oidvector
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   AND p.proconfig=ARRAY['search_path=pg_catalog, photo'];
  IF prior.oid IS NULL THEN RAISE EXCEPTION 'SEMANTIC_MANIFEST_SOURCE_SHAPE: %',item.signature; END IF;
  IF EXISTS(SELECT 1 FROM aclexplode(coalesce(prior.proacl,acldefault('f',prior.proowner))) a
    WHERE a.privilege_type='EXECUTE' AND a.grantee<>prior.proowner)
   OR EXISTS(SELECT 1 FROM pg_roles r WHERE NOT r.rolsuper AND r.oid<>prior.proowner
    AND NOT pg_has_role(r.oid,prior.proowner,'USAGE')
    AND has_function_privilege(r.oid,prior.oid,'EXECUTE'))
  THEN RAISE EXCEPTION 'SEMANTIC_MANIFEST_HELPER_EXPOSED: %',item.signature; END IF;
  IF (length(prior.prosrc)-length(replace(prior.prosrc,needle,'')))/length(needle)<>1
   OR strpos(prior.prosrc,replacement)>0
  THEN RAISE EXCEPTION 'SEMANTIC_MANIFEST_SOURCE_SHAPE: %',item.signature; END IF;
  revised:=replace(prior.prosrc,needle,replacement);
  EXECUTE replace(prior.definition,prior.prosrc,revised);
  IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=prior.oid AND p.prosrc=revised
   AND p.proowner=prior.proowner AND p.proacl IS NOT DISTINCT FROM prior.proacl
   AND p.proconfig IS NOT DISTINCT FROM prior.proconfig AND p.prosecdef=prior.prosecdef
   AND p.provolatile=prior.provolatile AND p.prorettype=prior.prorettype
   AND p.proargtypes=prior.proargtypes AND p.prokind=prior.prokind AND p.proretset=prior.proretset)
  THEN RAISE EXCEPTION 'SEMANTIC_MANIFEST_AUTHORITY_CHANGED: %',item.signature; END IF;
 END LOOP;
END $patch$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
