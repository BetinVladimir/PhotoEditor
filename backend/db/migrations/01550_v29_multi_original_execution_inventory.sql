-- +goose Up
-- Original occurrence readers use the same admitted execution plan as binding.
-- All existing artifact role/SHA/version/ordinal checks remain in place.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;old_text text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
 'photo.v29_adopted_multi_original_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text,integer,uuid,text,text)',
 'photo.v29_adopted_multi_original_inventory(uuid,uuid,uuid,uuid,uuid,text,uuid,text)'] LOOP
 definition:=pg_get_functiondef(signature::regprocedure);
 old_text:='SELECT r.config_snapshot->''v29_multi_plan'' INTO plan';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'MULTI_ORIGINAL_EXECUTION_PLAN_BASELINE_MISMATCH: %',signature;END IF;
 EXECUTE replace(definition,old_text,'SELECT photo.v29_multi_execution_plan(p_tenant,r.id) INTO plan');
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
