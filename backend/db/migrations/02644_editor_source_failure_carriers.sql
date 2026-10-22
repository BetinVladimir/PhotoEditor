-- +goose Up
-- Typed editor source failures belong to the scoped editor build, not a
-- nonexistent generic runs.errors column. Preserve all source/quality fences.
ALTER TABLE photo.editor_effect_builds ADD COLUMN intermediate_source_failure jsonb
 CHECK(intermediate_source_failure IS NULL OR intermediate_source_failure='{"code":"EDITOR_INTERMEDIATE_UNAVAILABLE"}'::jsonb);
-- +goose StatementBegin
CREATE FUNCTION photo.guard_editor_intermediate_source_failure() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.intermediate_source_failure IS NOT NULL THEN RAISE EXCEPTION 'EDITOR_INTERMEDIATE_FAILURE_REQUIRES_CURRENT_AUTHORITY' USING ERRCODE='42501'; END IF;
 ELSIF NEW.intermediate_source_failure IS DISTINCT FROM OLD.intermediate_source_failure
  AND (current_user<>'photo_fn_owner' OR OLD.intermediate_source_failure IS NOT NULL) THEN
  RAISE EXCEPTION 'EDITOR_INTERMEDIATE_FAILURE_IMMUTABLE' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_editor_intermediate_source_failure() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_editor_intermediate_source_failure() FROM PUBLIC;
CREATE TRIGGER editor_intermediate_source_failure_guard BEFORE INSERT OR UPDATE OF intermediate_source_failure
 ON photo.editor_effect_builds FOR EACH ROW EXECUTE FUNCTION photo.guard_editor_intermediate_source_failure();

DO $migration$
DECLARE item record; before record; after record; definition text; needle text; replacement text; expected_source text;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text)','9ef4741ad9d28cb3863827c94b883d0e2b909c6bb26a6a5d54e8eb5288ba374b','final'),
  ('photo.withdraw_editor_effect_build()','82bfee0a1a2d4dfb24e319df2262ad9e91ad2f02533dcb6e4839367f0ea2b9b3','intermediate')
 ) AS expected(signature,source_sha,kind) LOOP
  SELECT p.oid,p.proowner,p.proacl,p.proconfig,p.prosecdef,p.prolang,p.prorettype,p.pronargs,p.prosrc
   INTO STRICT before FROM pg_proc p WHERE p.oid=item.signature::regprocedure;
  IF encode(sha256(convert_to(before.prosrc,'UTF8')),'hex')<>item.source_sha
   OR before.proowner<>'photo_fn_owner'::regrole OR NOT before.prosecdef
   OR before.prolang<>(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   OR before.prorettype<>'boolean'::regtype
   OR ('search_path=pg_catalog, photo'=ANY(before.proconfig)
    OR 'search_path=pg_catalog,photo'=ANY(before.proconfig)) IS NOT TRUE
  THEN RAISE EXCEPTION 'EDITOR_FAILURE_CARRIER_PREIMAGE_DRIFT: %',item.signature; END IF;
  IF item.kind='final' THEN
   needle:=$source$ UPDATE photo.runs SET errors=errors||jsonb_build_array(jsonb_build_object('code',p_code,
  'message','An exact editor source object could not be verified. Start a new render from the original source.',
  'artifact_id',bound.id,'sha256',p_sha,'object_version',p_version))
 WHERE tenant_id=p_tenant AND id=p_build;$source$;
   replacement:='';
  ELSE
   needle:=$source$  UPDATE photo.runs SET errors=errors||jsonb_build_array(jsonb_build_object('code','EDITOR_INTERMEDIATE_UNAVAILABLE','message','An adopted editor intermediate is no longer available. Start a new render from the original source.'))
  WHERE tenant_id=b.tenant_id AND id=b.run_id AND NOT errors @> '[{"code":"EDITOR_INTERMEDIATE_UNAVAILABLE"}]'::jsonb;$source$;
   replacement:=$source$  UPDATE photo.editor_effect_builds SET intermediate_source_failure=jsonb_build_object('code','EDITOR_INTERMEDIATE_UNAVAILABLE'),updated_at=clock_timestamp()
  WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND intermediate_source_failure IS NULL;$source$;
  END IF;
  IF (length(before.prosrc)-length(replace(before.prosrc,needle,'')))/length(needle)<>1
  THEN RAISE EXCEPTION 'EDITOR_FAILURE_CARRIER_ANCHOR: %',item.signature; END IF;
  expected_source:=replace(before.prosrc,needle,replacement);
  definition:=pg_get_functiondef(before.oid);EXECUTE replace(definition,needle,replacement);
  SELECT p.oid,p.proowner,p.proacl,p.proconfig,p.prosecdef,p.prolang,p.prorettype,p.pronargs,p.prosrc
   INTO STRICT after FROM pg_proc p WHERE p.oid=item.signature::regprocedure;
  IF (to_jsonb(after)-'prosrc') IS DISTINCT FROM (to_jsonb(before)-'prosrc')
   OR after.prosrc IS DISTINCT FROM expected_source
  THEN RAISE EXCEPTION 'EDITOR_FAILURE_CARRIER_POSTIMAGE_DRIFT: %',item.signature; END IF;
 END LOOP;
END $migration$;

-- Public status exposes only closed stable reason codes, never object paths,
-- raw error text, session/actor data or issuer context. Existing run read
-- authorization remains in the service before this tenant-scoped accessor.
CREATE FUNCTION photo.editor_effect_source_failure_codes(p_tenant uuid,p_run uuid)
RETURNS text[] LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(array_agg(DISTINCT code ORDER BY code),'{}'::text[]) FROM (
  SELECT CASE WHEN b.intermediate_source_failure='{"code":"EDITOR_INTERMEDIATE_UNAVAILABLE"}'::jsonb
   THEN 'EDITOR_INTERMEDIATE_UNAVAILABLE' END code
  FROM photo.editor_effect_builds b WHERE b.tenant_id=p_tenant
   AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
   AND p_run IN(b.run_id,b.seal_run_id,b.final_run_id)
  UNION ALL
  SELECT b.final_source_failure->>'code' FROM photo.editor_effect_builds b
  WHERE b.tenant_id=p_tenant AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
   AND p_run IN(b.run_id,b.seal_run_id,b.final_run_id)
   AND b.final_source_failure->>'code' IN('EDITOR_SOURCE_OBJECT_MISSING','EDITOR_SOURCE_OBJECT_INTEGRITY')
 ) codes WHERE code IS NOT NULL
$$;
ALTER FUNCTION photo.editor_effect_source_failure_codes(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_source_failure_codes(uuid,uuid)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.editor_effect_source_failure_codes(uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02644 is forward-only'; END $$;
-- +goose StatementEnd
