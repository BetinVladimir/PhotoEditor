-- +goose Up
-- Stop new identity.compare graphs when the physically verifying adopter is
-- absent, even if an operator left the global registry row enabled.
-- +goose StatementBegin
ALTER FUNCTION photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)
 RENAME TO activate_v23_identity_compare_pre_adopter;
REVOKE ALL ON FUNCTION photo.activate_v23_identity_compare_pre_adopter(uuid,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.activate_v23_identity_compare(p_tenant uuid,p_run uuid,
 p_workflow uuid,p_task uuid,p_consents text) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM photo.v23_identity_adopter_state a WHERE
  a.singleton AND a.last_seen>clock_timestamp()-interval '30 seconds') OR
  NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE
   op.operation='identity.compare' AND op.version='1' AND
   op.resource_class='gpu' AND op.v2_enabled) THEN RETURN false;END IF;
 RETURN photo.activate_v23_identity_compare_pre_adopter(p_tenant,p_run,
  p_workflow,p_task,p_consents);
END $$;
ALTER FUNCTION photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
