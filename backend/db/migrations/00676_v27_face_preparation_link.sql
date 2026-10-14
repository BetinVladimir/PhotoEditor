-- +goose Up
-- The same canonical landmark producer must declare the original detector ref.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_face_preparation_link_matches(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_landmark uuid,p_landmark_sha text,p_detection uuid,p_detection_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT (SELECT count(*) FROM photo.semantic_proposal_face_preparation_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_landmark,p_landmark_sha,'face.landmarks'))=1
 AND (SELECT count(*) FROM photo.semantic_proposal_face_preparation_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_detection,p_detection_sha,'face.detect'))=1
 AND EXISTS(SELECT 1 FROM photo.artifacts root JOIN photo.tasks producer ON producer.tenant_id=root.tenant_id AND producer.id=root.created_by_task_id
 WHERE root.tenant_id=p_tenant AND root.id=p_landmark AND root.sha256=p_landmark_sha
 AND jsonb_typeof(producer.inputs)='array' AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_detection,'sha256',p_detection_sha)));
$$;
ALTER FUNCTION photo.semantic_proposal_face_preparation_link_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_face_preparation_link_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_face_preparation_link_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
