-- V23 profile creation: explicit project/person authority, no direct API INSERT.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.create_v23_profile(p_tenant uuid,p_project uuid,p_name text,
 p_mode text,p_person uuid,p_actor uuid,p_auth_session uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE new_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR length(coalesce(p_name,'')) NOT BETWEEN 1 AND 120 OR
  p_mode NOT IN('photographer','person') OR
  (p_mode='person') IS DISTINCT FROM (p_person IS NOT NULL) THEN
  RAISE EXCEPTION 'V23_PROFILE_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(
  p_tenant,p_project,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'V23_PROFILE_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 IF p_person IS NOT NULL AND NOT EXISTS(SELECT 1 FROM photo.project_persons p
  WHERE p.tenant_id=p_tenant AND p.project_id=p_project AND
   p.id=p_person AND p.state='active') THEN
  RAISE EXCEPTION 'V23_PROFILE_PERSON_INVALID' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.learning_profiles(tenant_id,project_id,name,mode,person_id,created_by)
 VALUES(p_tenant,p_project,p_name,p_mode,p_person,p_actor) RETURNING id INTO new_id;
 RETURN new_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_v23_profile(uuid,uuid,text,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v23_profile(uuid,uuid,text,text,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v23_profile(uuid,uuid,text,text,uuid,uuid,uuid) TO photo_api;
