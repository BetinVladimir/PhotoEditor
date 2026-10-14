-- +goose Up
-- Absence is meaningful only under the authenticated original detector anchor.
-- A present but invalid checkpoint is conflict, never permission to recreate it.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_input_stage_presence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_detector_sha text,p_operation text)
RETURNS text LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH anchor AS (
  SELECT * FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,
   photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference'),'face.detect')
  WHERE config_hash=p_detector_sha AND p_detector_sha ~ '^[0-9a-f]{64}$'
 ), selected AS (
  SELECT photo.semantic_uuid_v5(p_parent,'semantic.'||p_operation||':reference') AS run_id
  WHERE p_operation IN('face.landmarks','face.parse','mask.face_skin')
 )
 SELECT CASE
  WHEN NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=s.run_id) THEN 'absent'
  WHEN EXISTS(SELECT 1 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,s.run_id,p_operation)) THEN 'present'
  ELSE 'conflict' END
 FROM selected s WHERE EXISTS(SELECT 1 FROM anchor);
$$;
ALTER FUNCTION photo.semantic_native_input_stage_presence(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_input_stage_presence(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_input_stage_presence(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
