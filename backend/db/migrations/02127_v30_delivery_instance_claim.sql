-- +goose Up
-- A package requiring a signature can be claimed only by a scheduler instance
-- that advertised the exact frozen policy on the package's object bucket.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_expansion_instance_ready(
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
 RETURN EXISTS(
  SELECT 1 FROM photo.v30_delivery_runtime_leases l,
   LATERAL jsonb_array_elements(l.signing_policy_pins) pin(value)
  WHERE l.instance_id=p_instance AND l.bucket=p_bucket AND l.expires_at>clock_timestamp()
   AND tenant=ANY(l.signing_tenants)
   AND pin.value->>'tenant_id'=tenant::text
   AND pin.value->>'policy_id'=policy->>'signing_policy_id'
   AND pin.value->>'policy_sha'=policy->>'signing_policy_sha'
   AND (pin.value->>'not_before')::timestamptz<=clock_timestamp()
   AND (pin.value->>'not_after')::timestamptz>clock_timestamp());
EXCEPTION WHEN no_data_found OR too_many_rows OR invalid_text_representation
 OR invalid_datetime_format OR datetime_field_overflow THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_expansion_instance_ready(uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_expansion_instance_ready(uuid,text,uuid)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;

-- Reuse the existing claim and its locking order, inserting an additional
-- instance-scoped predicate after the current expansion readiness check.
-- +goose StatementBegin
DO $$
DECLARE definition text;old_name text;old_gate text;
BEGIN
 definition:=pg_get_functiondef('photo.claim_v30_delivery_expansion()'::regprocedure);
 old_name:='claim_v30_delivery_expansion()';
 old_gate:='OR photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true THEN CONTINUE;END IF;';
 IF definition IS NULL
 OR (length(definition)-length(replace(definition,old_name,'')))/length(old_name)<>1
 OR (length(definition)-length(replace(definition,old_gate,'')))/length(old_gate)<>1 THEN
  RAISE EXCEPTION 'Unexpected V30 delivery claim shape';END IF;
 definition:=replace(definition,old_name,
  'claim_v30_delivery_expansion_bound(p_instance uuid,p_bucket text)');
 definition:=replace(definition,old_gate,
  'OR photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true '
  ||'OR photo.v30_delivery_expansion_instance_ready(p_instance,p_bucket,e.id) IS DISTINCT FROM true '
  ||'THEN CONTINUE;END IF;');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_v30_delivery_expansion_bound(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v30_delivery_expansion_bound(uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v30_delivery_expansion_bound(uuid,text) TO photo_scheduler;
REVOKE EXECUTE ON FUNCTION photo.claim_v30_delivery_expansion() FROM photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
