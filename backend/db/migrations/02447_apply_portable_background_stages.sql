-- Portable background decode and strict automatic repair stages in Apply v4.
-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE definition text;patched text;
BEGIN
 definition:=pg_get_functiondef('photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb)'::regprocedure);
 patched:=replace(definition,'allowed constant text[]:=ARRAY[','allowed constant text[]:=ARRAY[''asset.inspect'',''asset.decode'',''defects.auto_repair'',''sensor.dust_auto_repair'',');
 IF patched=definition THEN RAISE EXCEPTION 'APPLY_V4_PLAN_ALLOWLIST_ANCHOR_MISSING';END IF;
 EXECUTE patched;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
