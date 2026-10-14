-- +goose Up
-- Optional selection must distinguish true absence under an authenticated
-- original detector from an unauthorized or damaged checkpoint.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_native_manual_selection_presence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_config_sha text)
RETURNS text LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE
  WHEN r.v27_initial_manual_selection IS NULL AND r.v27_initial_manual_selection_sha IS NULL THEN 'absent'
  WHEN r.v27_initial_manual_selection IS NOT NULL AND r.v27_initial_manual_selection_sha IS NOT NULL
   AND octet_length(r.v27_initial_manual_selection) BETWEEN 2 AND 65536
   AND r.v27_initial_manual_selection_sha=encode(public.digest(convert_to(r.v27_initial_manual_selection,'UTF8'),'sha256'),'hex') THEN 'present'
  ELSE 'conflict' END
 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference'),'face.detect') original
 JOIN photo.runs r ON r.tenant_id=p_tenant AND r.id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference')
 WHERE original.config_hash=p_config_sha AND p_config_sha ~ '^[0-9a-f]{64}$';
$$;
ALTER FUNCTION photo.v27_native_manual_selection_presence(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_native_manual_selection_presence(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_native_manual_selection_presence(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
