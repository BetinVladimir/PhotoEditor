-- +goose Up
-- Include the intent reference even when it is outside target snapshot.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_review_sources(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_revision bigint,p_reference uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE; r photo.runs%ROWTYPE;
BEGIN
 IF p_revision IS NULL OR p_revision<1 OR p_reference IS NULL THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='semantic';
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.asset_versions reference JOIN photo.assets asset ON asset.tenant_id=reference.tenant_id AND asset.id=reference.asset_id
  WHERE reference.tenant_id=p_tenant AND reference.id=p_reference AND asset.project_id=w.project_id) THEN RETURN false;END IF;
 -- Lifecycle, parent run, sorted tasks, then sorted owner resources by table/id.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR UPDATE;
 IF NOT FOUND OR r.status<>'waiting_review' OR r.cancel_requested OR NOT r.graph_sealed OR r.revision<>p_revision THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id ORDER BY id FOR UPDATE;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (
  EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
   WHERE i.tenant_id=p_tenant AND t.run_id=r.id AND i.artifact_id=a.id)
  OR EXISTS(SELECT 1 FROM photo.asset_versions reference WHERE reference.tenant_id=p_tenant AND reference.id=p_reference AND reference.master_artifact_id=a.id)
  OR EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   WHERE t.tenant_id=p_tenant AND t.run_id=r.id AND z.outputs->0->>'artifact_id'=a.id::text)
 ) ORDER BY a.id FOR SHARE;
 PERFORM v.id FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND (v.id=p_reference OR EXISTS(
  SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=w.input_snapshot_id AND i.asset_version_id=v.id
 )) ORDER BY v.id FOR SHARE;
 PERFORM asset.id FROM photo.assets asset WHERE asset.tenant_id=p_tenant AND (EXISTS(SELECT 1 FROM photo.asset_versions reference WHERE reference.tenant_id=p_tenant AND reference.id=p_reference AND reference.asset_id=asset.id) OR EXISTS(
  SELECT 1 FROM photo.snapshot_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
  WHERE i.tenant_id=p_tenant AND i.snapshot_id=w.input_snapshot_id AND v.asset_id=asset.id
 )) ORDER BY asset.id FOR SHARE;
 PERFORM id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor ORDER BY id FOR SHARE;
 PERFORM id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=w.project_id AND user_id=p_actor ORDER BY id FOR SHARE;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;
 RETURN photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session)
  AND EXISTS(SELECT 1 FROM photo.asset_versions reference JOIN photo.assets asset ON asset.tenant_id=reference.tenant_id AND asset.id=reference.asset_id
   JOIN photo.artifacts source ON source.tenant_id=reference.tenant_id AND source.id=reference.master_artifact_id AND source.sha256=reference.content_hash
   WHERE reference.tenant_id=p_tenant AND reference.id=p_reference AND asset.project_id=w.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
    AND source.state='ready' AND source.verified_at IS NOT NULL AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
    AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp()))
  AND photo.semantic_review_sources_current(p_tenant,p_workflow)
  AND EXISTS(SELECT 1 FROM photo.semantic_review_plan_sources(p_tenant,p_workflow,p_actor,p_session));
END $$;
ALTER FUNCTION photo.lock_semantic_review_sources(uuid,uuid,uuid,uuid,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_review_sources(uuid,uuid,uuid,uuid,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_review_sources(uuid,uuid,uuid,uuid,bigint,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
