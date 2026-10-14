-- +goose Up
-- Parent's original full policy digest is separate from content qualification.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_native_policy_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid,p_anchor jsonb,p_sha text)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_sha ~ '^[0-9a-f]{64}$' AND photo.semantic_defect_native_parent_ready(p_tenant,p_project,p_actor,p_session,p_task,p_anchor)
 AND EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND r.config_snapshot->'v27_defect_prepare'->>'policy_sha'=p_sha AND w.policy_sha=p_sha),false)
$$;
ALTER FUNCTION photo.semantic_defect_native_policy_ready(uuid,uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_native_policy_ready(uuid,uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_native_policy_ready(uuid,uuid,uuid,uuid,uuid,jsonb,text) TO photo_api;
CREATE FUNCTION photo.lock_semantic_defect_native_policy(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid,p_anchor jsonb,p_sha text,p_policy jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.lock_semantic_defect_native_parent(p_tenant,p_project,p_actor,p_session,p_task,p_anchor) IS NOT TRUE
 OR photo.lock_semantic_proposal_qualification_policy_snapshot(p_tenant,p_project,p_actor,p_session,p_policy) IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.semantic_defect_native_policy_ready(p_tenant,p_project,p_actor,p_session,p_task,p_anchor,p_sha);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_native_policy(uuid,uuid,uuid,uuid,uuid,jsonb,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_native_policy(uuid,uuid,uuid,uuid,uuid,jsonb,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_native_policy(uuid,uuid,uuid,uuid,uuid,jsonb,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
