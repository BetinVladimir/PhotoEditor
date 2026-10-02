-- Resolve V21 document dependencies only through project-bound provenance.
-- Tenant equality alone cannot authorize a user-supplied ArtifactRef.
-- +goose StatementBegin
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,
 p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_version IS NULL OR p_artifact IS NULL
  OR p_actor IS NULL OR p_session IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false;END IF;
 SELECT * INTO item FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact
  AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF NOT FOUND THEN RETURN false;END IF;
 IF p_contract='WorkingImage' THEN
  RETURN item.output_role='working_image' AND item.media_type='application/json' AND EXISTS(
   SELECT 1 FROM photo.asset_versions v JOIN photo.assets a
    ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=p_tenant AND a.project_id=p_project
    AND a.deleted_at IS NULL AND v.review_state='accepted'
    AND v.master_artifact_id=p_artifact AND v.content_hash=p_sha);
 ELSIF p_contract IN('WorkingPixels','WorkingAlpha') THEN
  RETURN item.output_role=CASE p_contract WHEN 'WorkingPixels' THEN 'working_rgb' ELSE 'alpha' END
   AND p_parent IS NOT NULL AND coalesce(p_parent_sha,'') ~ '^[0-9a-f]{64}$'
   AND EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a
    ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
    JOIN photo.artifacts source ON source.tenant_id=v.tenant_id
     AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
    WHERE v.tenant_id=p_tenant AND a.project_id=p_project
     AND v.review_state='accepted' AND source.id=p_parent AND source.sha256=p_parent_sha
     AND source.output_role='working_image'
     AND source.created_by_task_id=item.created_by_task_id
     AND source.produced_attempt=item.produced_attempt
     AND source.produced_fence=item.produced_fence
     AND item.created_by_task_id IS NOT NULL AND item.produced_attempt IS NOT NULL
     AND item.produced_fence IS NOT NULL);
 ELSIF p_contract='StyleV2' THEN
  RETURN item.media_type='application/json' AND EXISTS(
   SELECT 1 FROM photo.style_versions v JOIN photo.styles s
    ON s.tenant_id=v.tenant_id AND s.id=v.style_id
   WHERE v.tenant_id=p_tenant AND s.project_id=p_project AND s.archived_at IS NULL
    AND v.export_artifact_id=p_artifact AND v.config_hash=p_sha
    AND v.approval_status='accepted' AND v.schema_version='2.0.0'
    AND v.renderer_version='photo_renderer_v2');
 ELSIF p_contract NOT IN('LayerStack','MaskRef','Params','CorrectionPlan',
   'Override','MaskPixels','DenseWarpField','Artifact') THEN RETURN false;
 END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads u
   WHERE u.tenant_id=p_tenant AND u.project_id=p_project AND u.asset_version_id=p_version
    AND u.artifact_id=p_artifact AND u.sha256=p_sha AND u.state='ready')
  OR EXISTS(SELECT 1 FROM photo.v2_documents d JOIN photo.v2_document_streams s
   ON s.tenant_id=d.tenant_id AND s.id=d.stream_id
   WHERE d.tenant_id=p_tenant AND d.project_id=p_project AND s.asset_version_id=p_version
    AND d.artifact_id=p_artifact AND d.sha256=p_sha)
  OR EXISTS(SELECT 1 FROM photo.tasks task JOIN photo.runs r
   ON r.tenant_id=task.tenant_id AND r.id=task.run_id
   LEFT JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
   WHERE task.tenant_id=p_tenant AND task.id=item.created_by_task_id
    AND (w.project_id=p_project OR r.config_snapshot->>'project_id'=p_project::text))
  OR EXISTS(SELECT 1 FROM photo.style_asset_bindings b
   JOIN photo.style_versions v ON v.tenant_id=b.tenant_id AND v.id=b.style_version_id
   JOIN photo.styles s ON s.tenant_id=v.tenant_id AND s.id=v.style_id
   WHERE b.tenant_id=p_tenant AND s.project_id=p_project
    AND b.artifact_id=p_artifact AND b.sha256=p_sha);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;

-- Compatibility for scope probes that do not inspect nested pixel lineage.
-- +goose StatementBegin
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v2_draft_artifact_scope(p_tenant,p_project,p_version,p_artifact,
  p_sha,p_contract,p_actor,p_session,NULL::uuid,NULL::text)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid) TO photo_api;
