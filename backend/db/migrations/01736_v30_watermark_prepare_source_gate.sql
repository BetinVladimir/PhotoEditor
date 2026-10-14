-- +goose Up
-- Replace unconditional watermark refusal only with current persisted pin
-- authority. Metadata, snapshot, recipe, project and session checks stay intact.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_delivery_prepare_source_ready_before_tenant_context(uuid,uuid,uuid,uuid,text,text)'::regprocedure);
 needle:='OR (raw::jsonb)->''watermark'' IS NOT NULL THEN RETURN false;END IF;';
 replacement:='OR ((raw::jsonb)->''watermark'' IS NOT NULL AND photo.v30_watermark_pinned_source_ready(p_tenant,p_run,v_actor,v_session,(preset->>''preset_id'')::uuid,preset->>''preset_sha'',raw) IS DISTINCT FROM true) THEN RETURN false;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'WATERMARK_PREPARE_SOURCE_DEFINITION_DRIFT';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- Existing function OID/owner/ACL and tenant-context wrapper are preserved.
-- Child creation/activation still requires its own complete watermark protocol.
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
