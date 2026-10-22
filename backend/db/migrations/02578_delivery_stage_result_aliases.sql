-- +goose Up
-- Qualify result columns; preserve the function OID, authority and ACLs.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 SELECT pg_get_functiondef(p.oid) INTO definition FROM pg_proc p
 WHERE p.oid=to_regprocedure('photo.v30_delivery_stage_result(uuid,uuid,integer,text)')
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND p.proconfig=ARRAY['search_path=pg_catalog, photo'];
 needle:='SELECT * INTO z FROM photo.task_results WHERE tenant_id=e.tenant_id AND task_id=t.id AND id=t.canonical_result_id;';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'DELIVERY_STAGE_RESULT_ALIAS_SOURCE_SHAPE'; END IF;
 -- Other relation columns in this function do not collide with declared locals:
 -- task_id appears only on this result relation; outputs is always qualified.
 EXECUTE replace(definition,needle,
  'SELECT result.* INTO z FROM photo.task_results result WHERE result.tenant_id=e.tenant_id AND result.task_id=t.id AND result.id=t.canonical_result_id;');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
