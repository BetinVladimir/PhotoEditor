-- +goose Up
-- Dedicated blocked sharpen admission keeps current shared continuation logic.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.create_v30_delivery_continuation_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint)'::regprocedure);
 FOR needle,replacement IN SELECT * FROM (VALUES
 ('photo.create_v30_delivery_continuation_task(','photo.create_v30_watermark_sharpen_task('),
 ('photo.v30_delivery_params_scope(','photo.v30_watermark_sharpen_params_scope('),
 ('BEGIN',E'BEGIN\n IF p_stage IS DISTINCT FROM 1 OR p_task->>''operation'' IS DISTINCT FROM ''derivative.sharpen'' THEN RETURN false;END IF;')
 ) patches LOOP
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_SHARPEN_TASK_DEFINITION_DRIFT: %',needle;END IF;
  definition:=replace(definition,needle,replacement);
 END LOOP;
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
