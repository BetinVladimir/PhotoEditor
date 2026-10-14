-- +goose Up
-- Dedicated physical verification receipt; deliberately no v2-adopt marker.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 SELECT pg_get_functiondef('photo.record_v30_delivery_stage_verification(uuid,uuid,integer,text,jsonb,text,text)'::regprocedure) INTO definition;
 FOR needle,replacement IN SELECT * FROM (VALUES
  ('photo.record_v30_delivery_stage_verification(', 'photo.record_v30_watermark_stage_verification('),
  ('photo.v30_delivery_stage_result(', 'photo.v30_watermark_stage_result('),
  ('CASE p_operation WHEN ''derivative.resize'' THEN ''resize'' ELSE ''sharpen'' END', '''watermark'''),
  ('''v30-stage-verified''', '''v30-watermark-stage-verified''')
 ) patches LOOP
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark verification patch mismatch: %',needle;END IF;
  definition:=replace(definition,needle,replacement);
 END LOOP;
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.record_v30_watermark_stage_verification(uuid,uuid,integer,text,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v30_watermark_stage_verification(uuid,uuid,integer,text,jsonb,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.record_v30_watermark_stage_verification(uuid,uuid,integer,text,jsonb,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
