-- +goose Up
-- A revoked or malformed allowed_presets policy must refuse the item URL.
-- SQL OR evaluation cannot be relied on to avoid expanding a non-array JSONB.
-- +goose StatementBegin
DO $$
DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)'::regprocedure)
 INTO definition;
 IF definition IS NULL
  OR position('jsonb_array_elements(l.allowed_presets)' IN definition)=0
  OR position('jsonb_typeof(l.allowed_presets) IS DISTINCT FROM ''array''' IN definition)=0
  OR position('s.collection_id IS DISTINCT FROM j.collection_id' IN definition)=0
  OR position('v30_delivery_download_target' IN definition)=0 THEN
  RAISE EXCEPTION 'DOWNLOAD_POSTSIGN_PRESET_SHAPE_SOURCE';
 END IF;
 definition:=replace(definition,
  'jsonb_array_elements(l.allowed_presets)',
  'jsonb_array_elements(CASE WHEN jsonb_typeof(l.allowed_presets)=''array'' THEN l.allowed_presets ELSE ''[]''::jsonb END)');
 IF position('jsonb_array_elements(l.allowed_presets)' IN definition)>0 THEN
  RAISE EXCEPTION 'DOWNLOAD_POSTSIGN_PRESET_SHAPE_REWRITE';
 END IF;
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 TO photo_api,photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
