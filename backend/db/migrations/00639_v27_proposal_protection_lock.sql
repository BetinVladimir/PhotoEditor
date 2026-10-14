-- +goose Up
-- Canonical source-bound producer locks; policy coverage is a separate authority.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_protection(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producer uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false;END IF;
 SELECT created_by_task_id INTO producer FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root AND sha256=p_root_sha;
 IF producer IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(p_source,p_root,p_child) ORDER BY id FOR SHARE;
 RETURN photo.semantic_proposal_protection_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha);
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
