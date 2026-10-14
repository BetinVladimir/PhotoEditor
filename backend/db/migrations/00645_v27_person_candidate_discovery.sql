-- +goose Up
-- At most two candidates: callers reject ambiguity instead of picking newest.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_person_candidates(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_expected jsonb)
RETURNS TABLE(root_id uuid,root_sha text,child_id uuid,child_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT root.id,root.sha256,child.id,child.sha256
 FROM photo.artifacts root JOIN photo.tasks task ON task.tenant_id=root.tenant_id AND task.id=root.created_by_task_id
 JOIN photo.artifacts child ON child.tenant_id=root.tenant_id AND child.created_by_task_id=task.id
 WHERE root.tenant_id=p_tenant AND root.output_role='person_mask_ref' AND child.output_role='person_mask'
 AND task.operation='mask.person' AND task.operation_version='1'
 AND photo.semantic_proposal_person_profile_matches(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,root.id,root.sha256,child.id,child.sha256,p_expected)
 ORDER BY root.id,child.id LIMIT 2;
$$;
ALTER FUNCTION photo.semantic_proposal_person_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_person_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_person_candidates(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
