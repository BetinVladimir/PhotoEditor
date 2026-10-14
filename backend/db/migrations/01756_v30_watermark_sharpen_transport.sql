-- +goose Up
-- Registered sharpen transport under current watermark source authority.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_delivery_continuation_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text)'::regprocedure);
 FOR needle,replacement IN SELECT * FROM (VALUES
 ('photo.v30_delivery_continuation_transport_ready(','photo.v30_watermark_sharpen_transport_ready('),
 ('photo.v30_delivery_params_scope(','photo.v30_watermark_sharpen_params_scope('),
 ('BEGIN',E'BEGIN\n IF p_stage IS DISTINCT FROM 1 OR p_task->>''operation'' IS DISTINCT FROM ''derivative.sharpen'' THEN RETURN false;END IF;')
 ) patches LOOP
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_SHARPEN_TRANSPORT_DEFINITION_DRIFT: %',needle;END IF;
  definition:=replace(definition,needle,replacement);
 END LOOP;
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.v30_watermark_sharpen_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_sharpen_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_watermark_sharpen_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
