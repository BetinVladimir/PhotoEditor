-- +goose Up
-- Signing leases contain tenant identifiers even after expiry. Once object
-- cleanup is confirmed, the tenant purge must remove those historical pins.
-- A scheduler must not republish pins for a deleting/deleted tenant.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb)'::regprocedure);
 needle:=' INSERT INTO photo.v30_delivery_runtime_leases(instance_id,bucket,signing_tenants,signing_policy_pins,expires_at)';
 IF definition IS NULL OR
  (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 OR
  position('status=''active'' FOR SHARE' IN definition)>0 THEN
  RAISE EXCEPTION 'DELIVERY_RUNTIME_TENANT_ANNOUNCE_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,needle,
  ' IF (SELECT count(*) FROM (SELECT id FROM photo.tenants '
  ||'WHERE id=ANY(p_signing_tenants) AND status=''active'' FOR SHARE) active_tenants) '
  ||'IS DISTINCT FROM cardinality(p_signing_tenants) THEN RETURN false;END IF;'
  ||needle);
 EXECUTE definition;

 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);
 needle:=' SET CONSTRAINTS ALL DEFERRED;';
 IF definition IS NULL OR
  (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 OR
  position('UPDATE photo.v30_delivery_runtime_leases SET signing_tenants=' IN definition)>0 THEN
  RAISE EXCEPTION 'DELIVERY_RUNTIME_TENANT_PURGE_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,needle,needle||$purge$
 UPDATE photo.v30_delivery_runtime_leases SET
  signing_tenants=array_remove(signing_tenants,j.tenant_id),
  signing_policy_pins=(SELECT coalesce(jsonb_agg(pin.value ORDER BY pin.ordinal),'[]'::jsonb)
   FROM jsonb_array_elements(signing_policy_pins) WITH ORDINALITY AS pin(value,ordinal)
   WHERE pin.value->>'tenant_id' IS DISTINCT FROM j.tenant_id::text)
 WHERE j.tenant_id=ANY(signing_tenants) OR EXISTS(
  SELECT 1 FROM jsonb_array_elements(signing_policy_pins) pin(value)
  WHERE pin.value->>'tenant_id'=j.tenant_id::text);
$purge$);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
