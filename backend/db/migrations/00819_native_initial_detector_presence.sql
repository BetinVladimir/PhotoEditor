-- +goose Up
-- Scheduling hint only. Absence never substitutes complete publication,
-- source/intent/snapshot locking or detector begin/register/seal authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_native_initial_detector_presence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text)
RETURNS text LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE
 WHEN NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference')) THEN 'absent'
 WHEN EXISTS(SELECT 1 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference'),'face.detect') original
  WHERE original.config_snapshot->'v27_detector_prepare'->>'request_sha'=p_request_sha) THEN 'present'
 ELSE 'conflict' END
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND p_parent IS NOT NULL AND p_request_sha ~ '^[0-9a-f]{64}$';
$$;
ALTER FUNCTION photo.v27_native_initial_detector_presence(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_native_initial_detector_presence(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_native_initial_detector_presence(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
