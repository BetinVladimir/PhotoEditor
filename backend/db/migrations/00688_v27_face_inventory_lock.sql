-- +goose Up
-- Exact selected-profile inventory repeat under the existing tenant writer barrier.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_face_inventory(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_expected jsonb,p_inventory jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF jsonb_typeof(p_inventory) IS DISTINCT FROM 'array' OR pg_column_size(p_inventory)>1048576 THEN RETURN false; END IF;
 IF jsonb_array_length(p_inventory) NOT BETWEEN 1 AND 64 THEN RETURN false; END IF;
 IF photo.lock_semantic_proposal_person_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN false; END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('landmark',jsonb_build_object('artifact_id',candidate.landmark_id,'sha256',candidate.landmark_sha),'detection',jsonb_build_object('artifact_id',candidate.detection_id,'sha256',candidate.detection_sha)) ORDER BY candidate.landmark_id,candidate.detection_id),'[]'::jsonb) INTO actual
 FROM photo.semantic_proposal_face_preparation_candidates(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_expected) candidate;
 RETURN jsonb_array_length(actual)<=64 AND actual=p_inventory;
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_face_inventory(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_face_inventory(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_face_inventory(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
