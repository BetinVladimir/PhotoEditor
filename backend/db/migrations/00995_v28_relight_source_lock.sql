-- +goose Up
-- Prepublication source authority; producer/qualification/admission are separate.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_relight_source(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_base jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR SHARE;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project FOR SHARE;
 PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;
 PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor FOR SHARE;
 PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=p_project AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=p_snapshot FOR SHARE;
 PERFORM id FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=p_snapshot ORDER BY id FOR SHARE;
 PERFORM v.id FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND v.id IN
 (SELECT i.asset_version_id FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot)
 ORDER BY v.id FOR SHARE;
 PERFORM a.id FROM photo.assets a WHERE a.tenant_id=p_tenant AND a.id IN
 (SELECT v.asset_id FROM photo.asset_versions v JOIN photo.snapshot_items i ON i.tenant_id=v.tenant_id AND i.asset_version_id=v.id
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot) ORDER BY a.id FOR SHARE;
 PERFORM s.id FROM photo.artifacts s WHERE s.tenant_id=p_tenant AND s.id IN
 (SELECT i.artifact_id FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot
 UNION SELECT a.original_artifact_id FROM photo.assets a JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.asset_id=a.id
 JOIN photo.snapshot_items i ON i.tenant_id=v.tenant_id AND i.asset_version_id=v.id
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot) ORDER BY s.id FOR SHARE;
 RETURN photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v28_relight_source(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_relight_source(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_relight_source(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
