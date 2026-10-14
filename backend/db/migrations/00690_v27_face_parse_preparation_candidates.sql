-- +goose Up
-- Discover all selected profile-matching canonical parser outputs, without defects.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_face_parse_preparation_candidates(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_expected jsonb)
RETURNS TABLE(parse_id uuid,parse_sha text,detection_id uuid,detection_sha text,pixels_id uuid,pixels_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT manifest.id,manifest.sha256,detection.id,detection.sha256,pixels.id,pixels.sha256
 FROM photo.artifacts manifest
 JOIN photo.tasks parser ON parser.tenant_id=manifest.tenant_id AND parser.id=manifest.created_by_task_id
 JOIN photo.artifacts detection ON detection.tenant_id=manifest.tenant_id AND detection.output_role='face_boxes'
 JOIN photo.artifacts pixels ON pixels.tenant_id=manifest.tenant_id AND pixels.created_by_task_id=parser.id AND pixels.output_role='face_labels'
 WHERE manifest.tenant_id=p_tenant AND manifest.output_role='face_labels_ref'
 AND parser.operation='face.parse' AND parser.state='succeeded'
 AND parser.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha),jsonb_build_object('artifact_id',detection.id,'sha256',detection.sha256))
 AND photo.semantic_proposal_face_parse_preparation_profiles_match(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,manifest.id,manifest.sha256,detection.id,detection.sha256,pixels.id,pixels.sha256,p_expected) IS TRUE
 ORDER BY manifest.id,detection.id,pixels.id LIMIT 65;
$$;
ALTER FUNCTION photo.semantic_proposal_face_parse_preparation_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_face_parse_preparation_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_face_parse_preparation_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
