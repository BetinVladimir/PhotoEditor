-- +goose Up
-- Pins admission profile consistency; not independent worker measurements.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_person_profile_matches(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text,p_expected jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(photo.semantic_proposal_person_mask_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha)
 AND EXISTS(SELECT 1 FROM photo.artifacts root JOIN photo.tasks task ON task.tenant_id=root.tenant_id AND task.id=root.created_by_task_id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=task.tenant_id AND attempt.task_id=task.id AND attempt.attempt=task.attempt AND attempt.fencing_token=task.fencing_token
 WHERE root.tenant_id=p_tenant AND root.id=p_root AND root.sha256=p_root_sha
 AND p_expected->>'Operation'='mask.person' AND p_expected->>'Version'='1'
 AND attempt.backend=p_expected->>'Backend' AND attempt.finished_at IS NOT NULL
 AND task.execution_profile->>'backend'=p_expected->>'Backend'
 AND attempt.profile->>'backend'=p_expected->>'Backend'
 AND task.execution_profile->>'model_lock_sha'=p_expected->>'ModelLockSHA'
 AND attempt.profile->>'model_lock_sha'=p_expected->>'ModelLockSHA'
 AND task.execution_profile->>'runtime_lock_sha'=p_expected->>'RuntimeLockSHA'
 AND attempt.profile->>'runtime_lock_sha'=p_expected->>'RuntimeLockSHA'
 AND task.execution_profile->>'device_profile_sha'=p_expected->>'DeviceProfileSHA'
 AND attempt.profile->>'device_profile_sha'=p_expected->>'DeviceProfileSHA'),false);
$$;
ALTER FUNCTION photo.semantic_proposal_person_profile_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_person_profile_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_person_profile_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
