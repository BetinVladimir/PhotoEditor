-- +goose Up
-- The scheduling queue belongs to the outer run config. Embedded analysis plan,
-- descriptors and receipt recompilers remain byte-identical.
-- +goose StatementBegin
CREATE FUNCTION photo.v32_relight_analysis_queue_config(p_config jsonb,p_saved jsonb)
RETURNS jsonb LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
BEGIN
 IF jsonb_typeof(p_config) IS DISTINCT FROM 'object' OR p_config ? 'queue_class' THEN RETURN NULL;END IF;
 IF p_saved IS NOT NULL THEN
  IF jsonb_typeof(p_saved) IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
  IF NOT(p_saved ? 'queue_class') THEN RETURN p_config;END IF;
  IF p_saved->'queue_class' IS DISTINCT FROM '"interactive"'::jsonb THEN RETURN NULL;END IF;
 END IF;
 RETURN p_config||jsonb_build_object('queue_class','interactive');
END $$;
ALTER FUNCTION photo.v32_relight_analysis_queue_config(jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v32_relight_analysis_queue_config(jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.save_v28_relight_analysis_draft(uuid,uuid,uuid,text,text)'::regprocedure);
 needle:=' SELECT * INTO old FROM photo.runs WHERE id=v_run FOR UPDATE;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected relight analysis queue definition'; END IF;
 EXECUTE replace(definition,needle,needle||$patch$
 config:=photo.v32_relight_analysis_queue_config(config,old.config_snapshot);
 IF config IS NULL THEN RETURN false;END IF;
$patch$);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
