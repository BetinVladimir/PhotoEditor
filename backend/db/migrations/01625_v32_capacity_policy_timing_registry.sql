-- +goose Up
-- Global operator configuration, not tenant metadata or worker self-attestation.
CREATE TABLE photo.capacity_policy_timings (
 policy_sha text PRIMARY KEY CHECK(policy_sha ~ '^[0-9a-f]{64}$'),
 policy_jcs text NOT NULL CHECK(octet_length(policy_jcs) BETWEEN 2 AND 65536),
 offline_seconds bigint NOT NULL CHECK(offline_seconds BETWEEN 1 AND 9007199254740991),
 lease_seconds bigint NOT NULL CHECK(lease_seconds BETWEEN 1 AND 9007199254740991),
 CHECK(encode(sha256(convert_to(policy_jcs,'UTF8')),'hex')=policy_sha),
 CHECK(jsonb_typeof(policy_jcs::jsonb) IS NOT DISTINCT FROM 'object'),
 CHECK((policy_jcs::jsonb)->'schema_version' IS NOT DISTINCT FROM '1'::jsonb),
 CHECK((policy_jcs::jsonb)->'offline_seconds' IS NOT DISTINCT FROM to_jsonb(offline_seconds)),
 CHECK((policy_jcs::jsonb)->'lease_seconds' IS NOT DISTINCT FROM to_jsonb(lease_seconds)),
 CHECK(jsonb_typeof((policy_jcs::jsonb)->'heartbeat_seconds') IS NOT DISTINCT FROM 'number'
  AND ((policy_jcs::jsonb)->>'heartbeat_seconds')::numeric BETWEEN 1 AND 10
  AND trunc(((policy_jcs::jsonb)->>'heartbeat_seconds')::numeric)=((policy_jcs::jsonb)->>'heartbeat_seconds')::numeric
  AND ((policy_jcs::jsonb)->>'heartbeat_seconds')::numeric < offline_seconds),
 CHECK(jsonb_typeof((policy_jcs::jsonb)->'renew_seconds') IS NOT DISTINCT FROM 'number'
  AND ((policy_jcs::jsonb)->>'renew_seconds')::numeric BETWEEN 1 AND 10
  AND trunc(((policy_jcs::jsonb)->>'renew_seconds')::numeric)=((policy_jcs::jsonb)->>'renew_seconds')::numeric
  AND ((policy_jcs::jsonb)->>'renew_seconds')::numeric < lease_seconds)
);
ALTER TABLE photo.capacity_policy_timings OWNER TO photo_fn_owner;
REVOKE ALL ON TABLE photo.capacity_policy_timings FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
-- An installed SHA must never acquire different timing bytes. Operators rotate SHA.
-- +goose StatementBegin
CREATE FUNCTION photo.reject_capacity_policy_timing_update() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN RAISE EXCEPTION 'CAPACITY_POLICY_TIMING_IMMUTABLE' USING ERRCODE='23514'; END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reject_capacity_policy_timing_update() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reject_capacity_policy_timing_update() FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
CREATE TRIGGER capacity_policy_timings_immutable BEFORE UPDATE ON photo.capacity_policy_timings
FOR EACH ROW EXECUTE FUNCTION photo.reject_capacity_policy_timing_update();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
