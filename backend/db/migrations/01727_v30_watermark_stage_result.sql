-- +goose Up
-- Dedicated producer discovery preserves the existing output upload/fence checks.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_stage_result(uuid,uuid,integer,text)'::regprocedure) INTO definition;
 needle:='photo.v30_delivery_stage_result(';IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark producer name mismatch';END IF;
 definition:=replace(definition,needle,'photo.v30_watermark_stage_result(');
 needle:='IF p_operation IS NULL OR p_operation NOT IN(''derivative.resize'',''derivative.sharpen'') THEN RETURN NULL;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark producer operation mismatch';END IF;
 definition:=replace(definition,needle,'IF p_operation IS DISTINCT FROM ''derivative.watermark'' THEN RETURN NULL;END IF;');
 needle:='IF (p_operation=''derivative.resize'' AND cfg->''size'' IS NULL)
 OR (p_operation=''derivative.sharpen'' AND cfg->''sharpen'' IS NULL) THEN RETURN NULL;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark producer config mismatch';END IF;
 definition:=replace(definition,needle,'IF jsonb_typeof(cfg->''watermark'') IS DISTINCT FROM ''object'' THEN RETURN NULL;END IF;');
 needle:='SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;';
 replacement:=needle||' IF NOT EXISTS(SELECT 1 FROM photo.read_v30_watermark_pin(e.tenant_id,e.run_id,e.id,p_token,(expected->''preset''->>''preset_id'')::uuid,expected->''preset''->>''preset_sha'')) THEN RETURN NULL;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark producer authority mismatch';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
ALTER FUNCTION photo.v30_watermark_stage_result(uuid,uuid,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_stage_result(uuid,uuid,integer,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_watermark_stage_result(uuid,uuid,integer,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
