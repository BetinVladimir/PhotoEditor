-- +goose Up
-- REVOKE cannot remove a table owner's implicit DELETE privilege. Keep the
-- SECURITY DEFINER function owner separate from global process-state tables.
-- +goose StatementBegin
DO $$
DECLARE table_name text;owned_count integer;
 tables constant text[]:=ARRAY[
  'v30_import_runtime_leases','v30_import_adopter_runtime_leases',
  'v30_export_runtime_leases','v30_delivery_runtime_leases',
  'v31_signing_adopter_runtime_leases','capacity_policy_timings',
  'restore_quarantine_runs','restore_quarantine_versions'];
BEGIN
 IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='photo_runtime_owner') THEN
  CREATE ROLE photo_runtime_owner NOLOGIN NOINHERIT NOBYPASSRLS;
 END IF;
 IF EXISTS(SELECT FROM pg_roles WHERE rolname='photo_runtime_owner'
  AND (rolcanlogin OR rolsuper OR rolbypassrls OR rolinherit))
 OR EXISTS(SELECT FROM pg_auth_members membership
  JOIN pg_roles owner ON owner.oid=membership.roleid
  JOIN pg_roles member ON member.oid=membership.member
  WHERE owner.rolname='photo_runtime_owner' AND member.rolname IN
   ('photo_fn_owner','photo_api','photo_auth','photo_portal','photo_worker',
    'photo_scheduler','photo_gateway','photo_maintenance','photo_publisher')) THEN
  RAISE EXCEPTION 'RUNTIME_TABLE_OWNER_ROLE_UNSAFE';
 END IF;
 SELECT count(*) INTO owned_count FROM pg_class c
 JOIN pg_namespace namespace ON namespace.oid=c.relnamespace
 JOIN pg_roles owner ON owner.oid=c.relowner
 WHERE namespace.nspname='photo' AND c.relname=ANY(tables)
  AND c.relkind IN('r','p') AND owner.rolname='photo_fn_owner';
 IF owned_count<>cardinality(tables) THEN
  RAISE EXCEPTION 'RUNTIME_TABLE_OWNER_SOURCE_SHAPE';
 END IF;
 FOREACH table_name IN ARRAY tables LOOP
  EXECUTE format('ALTER TABLE photo.%I OWNER TO photo_runtime_owner',table_name);
 END LOOP;
END $$;
-- +goose StatementEnd

REVOKE ALL ON TABLE photo.v30_import_runtime_leases,
 photo.v30_import_adopter_runtime_leases,photo.v30_export_runtime_leases,
 photo.v30_delivery_runtime_leases,photo.v31_signing_adopter_runtime_leases,
 photo.capacity_policy_timings,photo.restore_quarantine_runs,
 photo.restore_quarantine_versions FROM photo_fn_owner;
GRANT SELECT,INSERT,UPDATE ON TABLE photo.v30_import_runtime_leases,
 photo.v30_import_adopter_runtime_leases,photo.v30_export_runtime_leases,
 photo.v30_delivery_runtime_leases,photo.v31_signing_adopter_runtime_leases
 TO photo_fn_owner;
GRANT SELECT ON TABLE photo.capacity_policy_timings,
 photo.restore_quarantine_runs,photo.restore_quarantine_versions
 TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
