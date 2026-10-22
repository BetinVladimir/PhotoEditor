-- +goose Up
-- Capability is advisory, but must not advertise a signer whose immutable
-- installation qualification has expired. Keep the existing qualification,
-- activation, backend, model-lock and capacity-policy predicates intact.
-- +goose StatementBegin
DO $$
DECLARE installed record; revised text;
 needle text:='AND (op.model_id IS NULL OR w.lock_id ~ ''^[0-9a-f]{64}$'')';
BEGIN
 SELECT p.oid,p.prosrc,pg_get_functiondef(p.oid) AS definition,p.proowner,p.proacl,p.proconfig
 INTO installed FROM pg_proc p WHERE p.oid=to_regprocedure('photo.tenant_capabilities()')
 AND p.prosecdef AND p.proowner='photo_fn_owner'::regrole
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo';
 IF installed.oid IS NULL OR strpos(installed.prosrc,'w.execution_pool')>0
 OR (length(installed.prosrc)-length(replace(installed.prosrc,needle,'')))/length(needle)<>1
 THEN RAISE EXCEPTION 'CAPABILITY_EXECUTION_POOL_SOURCE_SHAPE'; END IF;
 revised:=replace(installed.prosrc,needle,needle||'
   AND CASE WHEN op.operation=''provenance.sign'' THEN
    w.execution_pool=''signer'' AND w.signer_qualified_until>clock_timestamp()
    ELSE w.execution_pool=''general'' END');
 EXECUTE replace(installed.definition,installed.prosrc,revised);
 IF NOT EXISTS(SELECT FROM pg_proc p WHERE p.oid=installed.oid
  AND p.proowner=installed.proowner AND p.proacl IS NOT DISTINCT FROM installed.proacl
  AND p.proconfig IS NOT DISTINCT FROM installed.proconfig AND p.prosecdef)
 THEN RAISE EXCEPTION 'CAPABILITY_EXECUTION_POOL_AUTHORITY_CHANGED'; END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
