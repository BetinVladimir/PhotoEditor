-- +goose Up
-- Preserve persistence ABI/ACL and require exact durable source selection.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text:='SELECT * INTO source FROM photo.v30_delivery_watermark_source(p_tenant,project,(body->>''asset_id'')::uuid,(body->''binding''->>''asset_version_id'')::uuid,p_actor,p_session);';gate text;
BEGIN
 gate:='IF r.v30_watermark_selections->(scope->>''preset_id'') IS DISTINCT FROM jsonb_build_object(''asset_id'',body->''asset_id'',''asset_version_id'',body->''binding''->''asset_version_id'',''artifact_id'',body->''binding''->''source''->''artifact_id'',''sha256'',body->''binding''->''source''->''sha256'',''preset_sha256'',scope->''preset_sha256'') THEN RETURN false;END IF;';
 SELECT pg_get_functiondef('photo.pin_v30_watermark_source(uuid,uuid,uuid,uuid,text,text,text,text)'::regprocedure) INTO definition;
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark selection pin patch mismatch';END IF;
 EXECUTE replace(definition,needle,gate||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
