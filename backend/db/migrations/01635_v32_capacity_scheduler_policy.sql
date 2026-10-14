-- +goose Up
-- Scheduler reads an operator-installed digest, never self-installs configuration.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_queue_valid(p_policy jsonb)
RETURNS boolean LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(photo.capacity_policy_budget_valid(p_policy,'unknown_cost_seconds')
 AND photo.capacity_policy_budget_valid(p_policy,'tenant_heavy_queued')
 AND photo.capacity_policy_budget_valid(p_policy,'heavy_admission_inflight')
 AND CASE WHEN jsonb_typeof(p_policy->'queue_weights')='object' THEN
 (p_policy->'queue_weights'-'interactive'-'capture'-'batch'-'training')='{}'::jsonb
 AND (p_policy->'queue_weights') ?& ARRAY['interactive','capture','batch','training']
 AND NOT EXISTS(SELECT 1 FROM jsonb_each(p_policy->'queue_weights') e
 WHERE NOT photo.capacity_policy_budget_valid(p_policy->'queue_weights',e.key)
 OR CASE WHEN jsonb_typeof(e.value)='number' THEN (e.value::text)::numeric>100 ELSE true END)
 ELSE false END,false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_queue_valid(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_queue_valid(jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
ALTER TABLE photo.capacity_policy_timings ADD CONSTRAINT capacity_policy_queue_fields
 CHECK(photo.capacity_policy_queue_valid(policy_jcs::jsonb));
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_scheduler_policy(p_sha text) RETURNS text
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT policy_jcs FROM photo.capacity_policy_timings WHERE policy_sha=p_sha FOR SHARE;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_scheduler_policy(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_scheduler_policy(text) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.capacity_scheduler_policy(text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
