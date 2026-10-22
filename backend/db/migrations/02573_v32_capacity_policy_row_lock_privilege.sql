-- +goose Up
-- 02148 separates operator-owned timing rows from SECURITY DEFINER consumers.
-- PostgreSQL row-locking SELECT requires UPDATE on at least one column. Grant
-- that prerequisite only to the existing function owner, never runtime callers.
-- Immutable UPDATE trigger still refuses every actual value/no-op UPDATE;
-- INSERT/DELETE and ownership remain with the independent operator role.
-- +goose StatementBegin
DO $$
BEGIN
 IF NOT EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  JOIN pg_roles owner ON owner.oid=c.relowner WHERE n.nspname='photo'
  AND c.relname='capacity_policy_timings' AND c.relkind='r'
  AND owner.rolname='photo_runtime_owner')
 OR NOT EXISTS(SELECT FROM pg_trigger t WHERE t.tgrelid='photo.capacity_policy_timings'::regclass
  AND t.tgname='capacity_policy_timings_immutable' AND t.tgenabled='O'
  AND t.tgfoid='photo.reject_capacity_policy_timing_update()'::regprocedure
  AND t.tgtype=19)
 OR NOT EXISTS(SELECT FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
  WHERE p.oid='photo.reject_capacity_policy_timing_update()'::regprocedure
   AND owner.rolname='photo_fn_owner' AND NOT p.prosecdef
   AND p.prosrc LIKE '%CAPACITY_POLICY_TIMING_IMMUTABLE%') THEN
  RAISE EXCEPTION 'CAPACITY_POLICY_ROW_LOCK_SOURCE_SHAPE';
 END IF;
END $$;
-- +goose StatementEnd
GRANT UPDATE(policy_sha) ON photo.capacity_policy_timings TO photo_fn_owner;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
