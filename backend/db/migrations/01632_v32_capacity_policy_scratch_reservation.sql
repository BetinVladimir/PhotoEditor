-- +goose Up
-- Exact numeric arithmetic; operator multiplier applies once at reservation.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_scratch_valid(p_policy jsonb)
RETURNS boolean LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(CASE WHEN jsonb_typeof(p_policy->'scratch_multiplier')='number'
 THEN (p_policy->>'scratch_multiplier')::numeric BETWEEN 1 AND 4 ELSE false END,false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_scratch_valid(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_scratch_valid(jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
ALTER TABLE photo.capacity_policy_timings ADD CONSTRAINT capacity_policy_scratch_multiplier
 CHECK(photo.capacity_policy_scratch_valid(policy_jcs::jsonb));
-- NULL means unavailable/unsafe, never zero capacity or an overflowed integer.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_scratch_reservation(p_node photo.resource_nodes,p_bytes bigint)
RETURNS bigint LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH amount AS (
  SELECT CASE WHEN p_node.capacity_report_policy_sha IS NULL THEN p_bytes::numeric
   ELSE ceil(p_bytes::numeric*(c.policy_jcs::jsonb->>'scratch_multiplier')::numeric) END AS required
  FROM (SELECT 1) seed LEFT JOIN photo.capacity_policy_timings c ON c.policy_sha=p_node.capacity_report_policy_sha
 )
 SELECT CASE WHEN p_bytes BETWEEN 0 AND 9007199254740991 AND required BETWEEN 0 AND 9007199254740991
 THEN required::bigint ELSE NULL END FROM amount;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_scratch_reservation(photo.resource_nodes,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_scratch_reservation(photo.resource_nodes,bigint) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
-- Keep existing authoritative profile bounds, task identity, locks and ACLs.
-- Node row is already locked; the adjusted bytes feed both admission and INSERT.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'::regprocedure);
 needle:=' SELECT * INTO slot FROM photo.resource_slots WHERE node_id=p_node';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected scratch reservation definition'; END IF;
 EXECUTE replace(definition,needle,' p_scratch:=photo.capacity_policy_scratch_reservation(n,p_scratch);
 IF p_scratch IS NULL THEN RETURN QUERY SELECT ''waiting_resource'',NULL::jsonb; RETURN; END IF;
'||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
