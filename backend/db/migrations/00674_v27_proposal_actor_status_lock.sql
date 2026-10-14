-- +goose Up
-- Retain actor status through admission alongside session/membership scope.
-- Preserve existing tenant-then-usage lock ordering and final catalog recheck.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_semantic_proposal_inventory(p_tenant uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_canvas text,p_mask uuid,p_mask_sha text,p_snapshot uuid)
RETURNS TABLE(project_id uuid,item_count integer,ordinal integer,asset_version_id uuid,source_id uuid,source_sha text,canvas_hash text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;
BEGIN
 SELECT i.project_id INTO project FROM photo.semantic_proposal_inventory(p_tenant,p_actor,p_session,p_version,p_source,p_source_sha,p_canvas,p_mask,p_mask_sha,p_snapshot) i LIMIT 1;
 IF project IS NULL THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project FOR SHARE;
 PERFORM acl.user_id FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=project AND acl.user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=p_snapshot FOR SHARE;
 PERFORM a.id FROM photo.assets a WHERE a.tenant_id=p_tenant AND a.id IN(SELECT v.asset_id FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND (v.id=p_version OR v.id IN(SELECT i.asset_version_id FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot))) ORDER BY a.id FOR SHARE;
 PERFORM v.id FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND (v.id=p_version OR v.id IN(SELECT i.asset_version_id FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot)) ORDER BY v.id FOR SHARE;
 PERFORM u.document_id FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant AND u.artifact_id=p_mask ORDER BY u.document_id FOR SHARE;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (a.id=p_mask OR a.id IN(
 SELECT v.master_artifact_id FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND (v.id=p_version OR v.id IN(SELECT i.asset_version_id FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot))
 UNION SELECT asset.original_artifact_id FROM photo.assets asset JOIN photo.asset_versions v ON v.tenant_id=asset.tenant_id AND v.asset_id=asset.id WHERE v.tenant_id=p_tenant AND (v.id=p_version OR v.id IN(SELECT i.asset_version_id FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot)))) ORDER BY a.id FOR SHARE;
 RETURN QUERY SELECT * FROM photo.semantic_proposal_inventory(p_tenant,p_actor,p_session,p_version,p_source,p_source_sha,p_canvas,p_mask,p_mask_sha,p_snapshot);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.lock_semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
