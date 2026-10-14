-- +goose Up
-- Global policies have no tenant_id; tenant inventory locks cannot freeze them.
-- Writers take an exclusive transaction lock after statement mutation. Readers
-- take a shared lock and must re-discover exact policy inventory afterward.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v27_qualification_policy_write() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('photo.v27.qualification.policy.inventory',0));
 RETURN NULL;
END $$;
ALTER FUNCTION photo.guard_v27_qualification_policy_write() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v27_qualification_policy_write() FROM PUBLIC;
CREATE TRIGGER v27_qualification_policy_write AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON photo.v2_qualification_policies FOR EACH STATEMENT EXECUTE FUNCTION photo.guard_v27_qualification_policy_write();
CREATE FUNCTION photo.lock_semantic_proposal_qualification_inventory(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false; END IF;
 PERFORM pg_advisory_xact_lock_shared(hashtextextended('photo.v27.qualification.policy.inventory',0));
 RETURN photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session);
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_qualification_inventory(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_qualification_inventory(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_qualification_inventory(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
