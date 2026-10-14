-- +goose Up
-- Runtime lease rows are retained after withdraw/expiry. An old exact pin may
-- prove expiry even when the current signer deployment no longer loads it.
-- The replacement signer must still have a live tenant-scoped signing stage.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_expansion_instance_ready(
 p_instance uuid,p_bucket text,p_expansion uuid) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE policy jsonb;tenant uuid;
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_expansion IS NULL THEN RETURN false;END IF;
 SELECT e.tenant_id,(b.v30_delivery_proof->>'frozen_input_jcs')::jsonb->'policy'
 INTO STRICT tenant,policy
 FROM photo.graph_expansions e
 JOIN photo.tasks t ON t.tenant_id=e.tenant_id AND t.run_id=e.run_id AND t.id=e.parent_task_id
 JOIN photo.backend_documents b ON b.tenant_id=e.tenant_id AND b.run_id=e.run_id
  AND b.owner_id=t.id AND b.purpose='task_params' AND b.artifact_id=t.params_artifact_id
  AND b.state='ready'
 WHERE e.id=p_expansion AND e.kind='v2.delivery/1';
 IF photo.v31_delivery_frozen_signing_choice_valid(policy) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF policy->>'provenance'='off' THEN
  RETURN EXISTS(SELECT 1 FROM photo.v30_delivery_runtime_leases l
   WHERE l.instance_id=p_instance AND l.bucket=p_bucket AND l.expires_at>clock_timestamp());
 END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v30_delivery_runtime_leases l
  WHERE l.instance_id=p_instance AND l.bucket=p_bucket
  AND l.expires_at>clock_timestamp() AND tenant=ANY(l.signing_tenants)) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_delivery_runtime_leases l,
  LATERAL jsonb_array_elements(l.signing_policy_pins) pin(value)
  WHERE l.instance_id=p_instance AND l.bucket=p_bucket AND l.expires_at>clock_timestamp()
  AND pin.value->>'tenant_id'=tenant::text
  AND pin.value->>'policy_id'=policy->>'signing_policy_id'
  AND pin.value->>'policy_sha'=policy->>'signing_policy_sha'
  AND (pin.value->>'not_before')::timestamptz<=clock_timestamp()
  AND (pin.value->>'not_after')::timestamptz>clock_timestamp()) THEN RETURN true;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v30_delivery_runtime_leases historical,
   LATERAL jsonb_array_elements(historical.signing_policy_pins) pin(value)
  WHERE historical.bucket=p_bucket AND tenant=ANY(historical.signing_tenants)
  AND pin.value->>'tenant_id'=tenant::text
  AND pin.value->>'policy_id'=policy->>'signing_policy_id'
  AND pin.value->>'policy_sha'=policy->>'signing_policy_sha'
  AND (pin.value->>'not_after')::timestamptz<=clock_timestamp())
  AND photo.v30_delivery_signing_runtime_ready(p_bucket,tenant,
   (policy->>'signing_policy_id')::uuid,policy->>'signing_policy_sha') IS DISTINCT FROM true;
EXCEPTION WHEN no_data_found OR too_many_rows OR invalid_text_representation
 OR invalid_datetime_format OR datetime_field_overflow THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_expansion_instance_ready(uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_expansion_instance_ready(uuid,text,uuid)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;

-- Preserve the failure transition and its lock order from 02128. Only widen
-- expiry evidence from this live instance to a historical exact pin on the
-- same bucket, while requiring this instance's live tenant signing stage.
-- +goose StatementBegin
DO $$
DECLARE definition text;old_scope text;old_pin_scope text;
BEGIN
 definition:=pg_get_functiondef('photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid)'::regprocedure);
 old_scope:='WHERE l.instance_id=p_instance AND l.expires_at>clock_timestamp() FOR SHARE;';
 old_pin_scope:='WHERE l.instance_id=p_instance AND l.bucket=bucket AND l.expires_at>clock_timestamp()';
 IF definition IS NULL
 OR (length(definition)-length(replace(definition,old_scope,'')))/length(old_scope)<>1
 OR (length(definition)-length(replace(definition,old_pin_scope,'')))/length(old_pin_scope)<>1 THEN
  RAISE EXCEPTION 'Unexpected V31 expired-policy failure shape';END IF;
 definition:=replace(definition,old_scope,
  'WHERE l.instance_id=p_instance AND l.expires_at>clock_timestamp() '
  ||'AND x.tenant_id=ANY(l.signing_tenants) FOR SHARE;');
 definition:=replace(definition,old_pin_scope,'WHERE l.bucket=bucket');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
