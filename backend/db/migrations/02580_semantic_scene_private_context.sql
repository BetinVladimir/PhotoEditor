-- +goose Up
-- Internal lease/adoption callers already bind their exact tenant. Keep the
-- session tenant boundary on public graph sealing, not on its owner-only helper.
-- +goose StatementBegin
DO $patch$
DECLARE item record; before_value record; revised text; needle text;
BEGIN
 FOR item IN SELECT * FROM (VALUES
 ('photo.v27_native_scene_graph_ready(uuid,uuid)',true),
 ('photo.seal_v2_graph(uuid,uuid)',false)) AS changes(signature,private_helper) LOOP
  SELECT p.oid,p.prosrc,pg_get_functiondef(p.oid) definition,p.proowner,p.proacl,p.proconfig
  INTO before_value FROM pg_proc p WHERE p.oid=to_regprocedure(item.signature)
  AND p.prosecdef AND p.proowner='photo_fn_owner'::regrole
  AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
  AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo';
  IF before_value.oid IS NULL THEN RAISE EXCEPTION 'SEMANTIC_PRIVATE_CONTEXT_SOURCE_SHAPE';END IF;
  IF item.private_helper THEN
   IF EXISTS(SELECT 1 FROM aclexplode(coalesce(before_value.proacl,acldefault('f',before_value.proowner))) a
    WHERE a.privilege_type='EXECUTE' AND a.grantee<>before_value.proowner)
   THEN RAISE EXCEPTION 'SEMANTIC_PRIVATE_CONTEXT_HELPER_EXPOSED';END IF;
   needle:=' IF p_tenant IS DISTINCT FROM nullif(current_setting(''app.tenant_id'',true),'''')::uuid THEN RETURN false;END IF;';
   IF (length(before_value.prosrc)-length(replace(before_value.prosrc,needle,'')))/length(needle)<>1
   THEN RAISE EXCEPTION 'SEMANTIC_PRIVATE_CONTEXT_SOURCE_SHAPE';END IF;
   revised:=replace(before_value.prosrc,needle,'');
  ELSE
   needle:='BEGIN
 IF photo.v27_native_scene_graph_ready(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_frozen_scene(p_tenant,p_run);
END';
   IF btrim(before_value.prosrc,E' \t\n\r')<>needle THEN RAISE EXCEPTION 'SEMANTIC_PUBLIC_SEAL_SOURCE_SHAPE';END IF;
   revised:=E'\nBEGIN\n IF p_tenant IS DISTINCT FROM nullif(current_setting(''app.tenant_id'',true),'''')::uuid THEN RETURN false;END IF;\n IF photo.v27_native_scene_graph_ready(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN false;END IF;\n RETURN photo.seal_v2_graph_pre_frozen_scene(p_tenant,p_run);\nEXCEPTION WHEN invalid_text_representation THEN RETURN false;\nEND\n';
  END IF;
  EXECUTE replace(before_value.definition,before_value.prosrc,revised);
  IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=before_value.oid
   AND p.proowner=before_value.proowner AND p.proacl IS NOT DISTINCT FROM before_value.proacl
   AND p.proconfig IS NOT DISTINCT FROM before_value.proconfig AND p.prosecdef)
  THEN RAISE EXCEPTION 'SEMANTIC_PRIVATE_CONTEXT_AUTHORITY_CHANGED';END IF;
 END LOOP;
END $patch$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
