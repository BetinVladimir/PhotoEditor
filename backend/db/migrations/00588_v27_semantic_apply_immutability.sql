-- +goose Up
-- Freeze semantic Apply identity from creation and full execution profile after
-- input seal. Legacy Apply without approved_semantic retains existing behavior.
-- Runtime state/attempt/fence/cancellation may still advance normally.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_semantic_apply_config() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF (OLD.kind='apply' AND OLD.config_snapshot ? 'approved_semantic')
 OR (NEW.kind='apply' AND NEW.config_snapshot ? 'approved_semantic') THEN
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
  OR NEW.input_snapshot_id IS DISTINCT FROM OLD.input_snapshot_id
  OR NEW.config_snapshot IS DISTINCT FROM OLD.config_snapshot OR NEW.config_hash IS DISTINCT FROM OLD.config_hash
  OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.failure_policy IS DISTINCT FROM OLD.failure_policy THEN
   RAISE EXCEPTION 'SEMANTIC_APPLY_CONFIG_IMMUTABLE' USING ERRCODE='55000';
  END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_semantic_apply_config() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_semantic_apply_config() FROM PUBLIC;
CREATE TRIGGER semantic_apply_config_immutable BEFORE UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_semantic_apply_config();
CREATE FUNCTION photo.guard_semantic_apply_task_profile() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.execution_profile ? 'v2_input_manifest' AND EXISTS(
 SELECT 1 FROM photo.runs r WHERE r.tenant_id=OLD.tenant_id AND r.id=OLD.run_id
 AND r.kind='apply' AND r.config_snapshot ? 'approved_semantic') THEN
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.run_id IS DISTINCT FROM OLD.run_id
  OR NEW.node_key IS DISTINCT FROM OLD.node_key OR NEW.operation IS DISTINCT FROM OLD.operation
  OR NEW.operation_version IS DISTINCT FROM OLD.operation_version OR NEW.resource_class IS DISTINCT FROM OLD.resource_class
  OR NEW.params_artifact_id IS DISTINCT FROM OLD.params_artifact_id OR NEW.inputs IS DISTINCT FROM OLD.inputs
  OR NEW.input_digest IS DISTINCT FROM OLD.input_digest OR NEW.execution_profile IS DISTINCT FROM OLD.execution_profile THEN
   RAISE EXCEPTION 'SEMANTIC_APPLY_TASK_PROFILE_IMMUTABLE' USING ERRCODE='55000';
  END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_semantic_apply_task_profile() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_semantic_apply_task_profile() FROM PUBLIC;
CREATE TRIGGER semantic_apply_task_profile_immutable BEFORE UPDATE ON photo.tasks
 FOR EACH ROW EXECUTE FUNCTION photo.guard_semantic_apply_task_profile();
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
