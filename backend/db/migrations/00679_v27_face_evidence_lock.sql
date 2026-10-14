-- +goose Up
-- Selected canonical face producer/content pins; caller composes source/auth,
-- qualification and complete inventory locks before admitting queued work.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_face_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_landmark uuid,p_landmark_sha text,p_detection uuid,p_detection_sha text,p_expected jsonb,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producers uuid[]; landmark_object record; detector_object record; actual jsonb;
BEGIN
 IF jsonb_typeof(p_evidence) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 IF jsonb_array_length(p_evidence)<>4 THEN RETURN false; END IF;
 IF NOT photo.semantic_proposal_face_preparation_profiles_match(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_landmark,p_landmark_sha,p_detection,p_detection_sha,p_expected) THEN RETURN false; END IF;
 SELECT array_agg(DISTINCT created_by_task_id ORDER BY created_by_task_id) INTO producers FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(p_landmark,p_detection);
 IF coalesce(array_length(producers,1),0)<>2 THEN RETURN false; END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(producers) ORDER BY id FOR SHARE;
 PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=ANY(producers) ORDER BY task_id,attempt FOR SHARE;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=ANY(producers) ORDER BY id FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND (id=p_source OR created_by_task_id=ANY(producers) OR id IN(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(producers))) ORDER BY id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(producers)) ORDER BY id FOR SHARE;
 PERFORM run_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id IN(SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(producers)) ORDER BY run_id FOR SHARE;
 IF NOT photo.semantic_proposal_face_preparation_profiles_match(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_landmark,p_landmark_sha,p_detection,p_detection_sha,p_expected) THEN RETURN false; END IF;
 SELECT * INTO landmark_object FROM photo.semantic_proposal_face_preparation_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_landmark,p_landmark_sha,'face.landmarks');
 SELECT * INTO detector_object FROM photo.semantic_proposal_face_preparation_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_detection,p_detection_sha,'face.detect');
 SELECT jsonb_agg(jsonb_build_object('role',pins.role,'artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) ORDER BY pins.role) INTO actual
 FROM (VALUES('face_landmarks',p_landmark),('landmark_params',landmark_object.params_id),('face_boxes',p_detection),('detector_params',detector_object.params_id)) pins(role,id)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=pins.id;
 RETURN coalesce(actual=p_evidence AND jsonb_array_length(actual)=4,false);
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_face_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_face_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_face_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
