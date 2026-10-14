-- +goose Up
-- Discover canonical source-bound face collections without defect outputs.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_face_preparation_candidates(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_expected jsonb)
RETURNS TABLE(landmark_id uuid,landmark_sha text,detection_id uuid,detection_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT landmark.id,landmark.sha256,detection.id,detection.sha256
 FROM photo.artifacts landmark
 JOIN photo.tasks producer ON producer.tenant_id=landmark.tenant_id AND producer.id=landmark.created_by_task_id
 JOIN photo.artifacts detection ON detection.tenant_id=landmark.tenant_id AND detection.output_role='face_boxes'
 WHERE landmark.tenant_id=p_tenant AND landmark.output_role='face_landmarks'
 AND producer.operation='face.landmarks' AND producer.state='succeeded'
 AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha),jsonb_build_object('artifact_id',detection.id,'sha256',detection.sha256))
 AND photo.semantic_proposal_face_preparation_profiles_match(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,landmark.id,landmark.sha256,detection.id,detection.sha256,p_expected) IS TRUE
 ORDER BY landmark.id,detection.id LIMIT 65;
$$;
ALTER FUNCTION photo.semantic_proposal_face_preparation_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_face_preparation_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_face_preparation_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
