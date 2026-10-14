-- +goose Up
-- Selected exact stored profiles only; original trusted reports remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_face_preparation_profiles_match(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_landmark uuid,p_landmark_sha text,p_detection uuid,p_detection_sha text,p_expected jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.semantic_proposal_face_preparation_link_matches(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_landmark,p_landmark_sha,p_detection,p_detection_sha)
 AND jsonb_array_length(CASE WHEN jsonb_typeof(p_expected)='array' THEN p_expected ELSE '[]'::jsonb END)=2
 AND (SELECT count(*)=2
 FROM (VALUES('face.landmarks'::text,p_landmark,p_expected->0),('face.detect'::text,p_detection,p_expected->1)) requested(operation,output_id,expected)
 JOIN photo.artifacts output ON output.tenant_id=p_tenant AND output.id=requested.output_id
 JOIN photo.tasks producer ON producer.tenant_id=output.tenant_id AND producer.id=output.created_by_task_id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
 WHERE requested.expected->>'Operation'=requested.operation AND requested.expected->>'Version'='1'
 AND requested.expected->>'ModelLockSHA' ~ '^[0-9a-f]{64}$' AND requested.expected->>'RuntimeLockSHA' ~ '^[0-9a-f]{64}$' AND requested.expected->>'DeviceProfileSHA' ~ '^[0-9a-f]{64}$'
 AND attempt.finished_at IS NOT NULL AND attempt.backend=requested.expected->>'Backend'
 AND producer.execution_profile->>'backend'=requested.expected->>'Backend' AND attempt.profile->>'backend'=requested.expected->>'Backend'
 AND producer.execution_profile->>'model_lock_sha'=requested.expected->>'ModelLockSHA' AND attempt.profile->>'model_lock_sha'=requested.expected->>'ModelLockSHA'
 AND producer.execution_profile->>'runtime_lock_sha'=requested.expected->>'RuntimeLockSHA' AND attempt.profile->>'runtime_lock_sha'=requested.expected->>'RuntimeLockSHA'
 AND producer.execution_profile->>'device_profile_sha'=requested.expected->>'DeviceProfileSHA' AND attempt.profile->>'device_profile_sha'=requested.expected->>'DeviceProfileSHA');
$$;
ALTER FUNCTION photo.semantic_proposal_face_preparation_profiles_match(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_face_preparation_profiles_match(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_face_preparation_profiles_match(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
