-- +goose Up
-- A cached report cannot survive a change in registration identity/authority.
-- Does not change immutable task/run snapshots or model parameters.
-- +goose StatementBegin
CREATE FUNCTION photo.invalidate_worker_model_residency() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF ROW(NEW.worker_id,NEW.tenant_id,NEW.node_id,NEW.db_principal,NEW.enabled,
 NEW.capacity_policy_sha,NEW.lock_id,NEW.backends,NEW.resource_classes,NEW.qualified_operations)
 IS DISTINCT FROM ROW(OLD.worker_id,OLD.tenant_id,OLD.node_id,OLD.db_principal,OLD.enabled,
 OLD.capacity_policy_sha,OLD.lock_id,OLD.backends,OLD.resource_classes,OLD.qualified_operations) THEN
  NEW.model_residency:='[]'::jsonb;
  NEW.model_residency_reported_at:=NULL;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.invalidate_worker_model_residency() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.invalidate_worker_model_residency() FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
CREATE TRIGGER worker_model_residency_authority
 BEFORE UPDATE ON photo.worker_registrations
 FOR EACH ROW EXECUTE FUNCTION photo.invalidate_worker_model_residency();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
