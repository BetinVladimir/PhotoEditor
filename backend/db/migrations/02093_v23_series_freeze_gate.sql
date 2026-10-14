-- Series evidence is transportable but not yet publishable. Refuse any
-- manifest that declares it until the immutable document, Fit input closure
-- and same-renderer Evaluate path are installed. Preserve prior gates.
-- +goose Up
ALTER FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 RENAME TO v23_training_frozen_complete_pre_series_core;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_complete_pre_series_core(uuid,uuid,jsonb)
 FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_frozen_complete(
 p_tenant uuid,p_run uuid,p_manifest jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR
  jsonb_typeof(p_manifest->'examples') IS DISTINCT FROM 'array' OR
  EXISTS(SELECT 1 FROM jsonb_array_elements(p_manifest->'examples') item
   WHERE jsonb_typeof(item.value->'render_case') IS DISTINCT FROM 'object'
    OR (item.value->'render_case') ? 'series_evidence') THEN
  RETURN false;
 END IF;
 RETURN photo.v23_training_frozen_complete_pre_series_core(
  p_tenant,p_run,p_manifest);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
