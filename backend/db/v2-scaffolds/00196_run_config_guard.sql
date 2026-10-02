-- Admission-pinned V2 identity and configuration are immutable. Runtime
-- status, revision, graph_sealed, cancellation and quota fields remain mutable.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v2_run_config() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF (OLD.kind='v2' OR NEW.kind='v2') AND (
  NEW.kind IS DISTINCT FROM OLD.kind OR
  NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR
  NEW.input_snapshot_id IS DISTINCT FROM OLD.input_snapshot_id OR
  NEW.config_snapshot IS DISTINCT FROM OLD.config_snapshot OR
  NEW.config_hash IS DISTINCT FROM OLD.config_hash OR
  NEW.created_by IS DISTINCT FROM OLD.created_by OR
  NEW.failure_policy IS DISTINCT FROM OLD.failure_policy) THEN
  RAISE EXCEPTION 'V2_RUN_CONFIG_IMMUTABLE' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v2_run_config() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v2_run_config() FROM PUBLIC;
CREATE TRIGGER v2_run_config_immutable BEFORE UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_run_config();
