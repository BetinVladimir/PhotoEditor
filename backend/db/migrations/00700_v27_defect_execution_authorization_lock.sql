-- +goose Up
-- Hold live principal/project/source authority through caller admission TX.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_execution_scope(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.users WHERE id=p_actor AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor AND revoked_at IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=p_project AND user_id=p_actor FOR SHARE;
 PERFORM asset.id FROM photo.assets asset JOIN photo.asset_versions version ON version.tenant_id=asset.tenant_id AND version.asset_id=asset.id
 WHERE version.tenant_id=p_tenant AND version.id=p_version AND asset.project_id=p_project
 AND asset.deleted_at IS NULL AND asset.upload_state='verified' FOR SHARE OF asset;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=p_version AND review_state='accepted'
 AND master_artifact_id=p_source AND content_hash=p_sha FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_source AND sha256=p_sha
 AND state='ready' AND verified_at IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 RETURN photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.sessions WHERE id=p_session AND user_id=p_actor AND revoked_at IS NULL AND expires_at>clock_timestamp());
END $$;
ALTER FUNCTION photo.lock_semantic_defect_execution_scope(uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_execution_scope(uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_execution_scope(uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
