-- V23 scheduler-only inventory metadata. The candidate function retains
-- its tenant/profile locks until the caller's transaction commits.
-- +goose Up
GRANT EXECUTE ON FUNCTION photo.v23_training_candidate_rows(
 uuid,uuid,bigint,uuid[],uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_inventory_profile(
 p_tenant uuid,p_profile uuid,p_actor uuid,p_auth_session uuid)
RETURNS TABLE(project_id uuid,mode text,person_id uuid)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_profile IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL THEN
  RAISE EXCEPTION 'V23_TRAIN_PROFILE_INVALID' USING ERRCODE='22023';
 END IF;
 RETURN QUERY SELECT profile.project_id,profile.mode,profile.person_id
 FROM photo.learning_profiles profile
 JOIN photo.tenants tenant_row ON tenant_row.id=profile.tenant_id
 WHERE profile.tenant_id=p_tenant AND profile.id=p_profile
  AND tenant_row.status='active'
  AND photo.v2_editor_authorized(p_tenant,profile.project_id,
   p_actor,p_auth_session);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_inventory_profile(uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_inventory_profile(uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_inventory_profile(uuid,uuid,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
