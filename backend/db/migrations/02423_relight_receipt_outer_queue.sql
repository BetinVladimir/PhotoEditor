-- +goose Up
-- V32 scheduling metadata must not change the exact V28 authored request.
-- +goose StatementBegin
CREATE FUNCTION photo.v32_relight_receipt_authored_config(p_config jsonb)
RETURNS jsonb LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
BEGIN
 IF jsonb_typeof(p_config) IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 IF NOT(p_config ? 'queue_class') THEN RETURN p_config;END IF;
 IF p_config->'queue_class' IS DISTINCT FROM '"interactive"'::jsonb THEN RETURN NULL;END IF;
 RETURN p_config-'queue_class';
END $$;
ALTER FUNCTION photo.v32_relight_receipt_authored_config(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v32_relight_receipt_authored_config(jsonb)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.read_v28_relight_request_receipt(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb)'::regprocedure);
 needle:=' IF config IS DISTINCT FROM jsonb_build_object(';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected relight receipt config comparison';END IF;
 EXECUTE replace(definition,needle,' IF photo.v32_relight_receipt_authored_config(config) IS DISTINCT FROM jsonb_build_object(');
 definition:=pg_get_functiondef('photo.read_v28_relight_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb)'::regprocedure);
 needle:=' IF r.config_snapshot IS DISTINCT FROM jsonb_build_object(';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected strict relight receipt config comparison';END IF;
 EXECUTE replace(definition,needle,' IF photo.v32_relight_receipt_authored_config(r.config_snapshot) IS DISTINCT FROM jsonb_build_object(');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
