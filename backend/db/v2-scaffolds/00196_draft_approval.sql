-- FrozenRecipe bytes are produced and validated by the service before this DB TX.
-- +goose StatementBegin
CREATE FUNCTION photo.approve_v2_draft(
 p_tenant uuid,p_asset_version uuid,p_expected bigint,p_document uuid,p_actor uuid,p_session uuid,
 p_resolved_artifact uuid,p_resolved_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid; asset uuid; latest_version uuid; stream photo.v2_document_streams%ROWTYPE;
 recipe photo.v2_documents%ROWTYPE; approval photo.v2_document_approvals%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_asset_version IS NULL OR p_document IS NULL OR p_actor IS NULL
  OR p_session IS NULL OR p_resolved_artifact IS NULL OR p_expected IS NULL OR p_expected<1
  OR coalesce(p_resolved_sha,'') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V2_APPROVAL_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V2_TENANT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 SELECT a.project_id,a.id INTO project,asset FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 WHERE v.tenant_id=p_tenant AND v.id=p_asset_version AND v.review_state='accepted'
  AND a.deleted_at IS NULL AND a.upload_state='verified'
  FOR UPDATE OF a FOR SHARE OF v;
 IF project IS NULL OR NOT photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) THEN
  RAISE EXCEPTION 'V2_APPROVAL_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT id INTO latest_version FROM photo.asset_versions
  WHERE tenant_id=p_tenant AND asset_id=asset ORDER BY version_no DESC LIMIT 1;
 IF latest_version IS DISTINCT FROM p_asset_version THEN
  RAISE EXCEPTION 'V2_APPROVAL_SOURCE_STALE' USING ERRCODE='40001';
 END IF;
 SELECT * INTO stream FROM photo.v2_document_streams
  WHERE tenant_id=p_tenant AND project_id=project AND asset_version_id=p_asset_version
   AND stream_type='draft' FOR UPDATE;
 IF NOT FOUND OR stream.current_document_id IS DISTINCT FROM p_document THEN
  RAISE EXCEPTION 'V2_APPROVAL_DOCUMENT_CONFLICT' USING ERRCODE='40001';
 END IF;
 SELECT * INTO recipe FROM photo.v2_documents
  WHERE tenant_id=p_tenant AND project_id=project AND stream_id=stream.id
   AND id=p_document AND document_type='EditingRecipeV3';
 IF NOT FOUND OR NOT EXISTS(
  SELECT 1 FROM photo.v2_draft_uploads u JOIN photo.artifacts a
   ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id AND a.sha256=u.sha256
  WHERE u.tenant_id=p_tenant AND u.project_id=project
   AND u.asset_version_id=p_asset_version AND u.document_id<>p_document
   AND u.artifact_id=p_resolved_artifact AND u.sha256=p_resolved_sha
   AND u.state='ready' AND a.state='ready' AND a.output_role='document'
   AND a.media_type='application/json' AND a.object_version=u.object_version
   AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'V2_APPROVAL_ARTIFACT_INVALID' USING ERRCODE='23514';
 END IF;
 SELECT * INTO approval FROM photo.v2_document_approvals
  WHERE tenant_id=p_tenant AND document_id=p_document;
 IF FOUND THEN
  IF stream.approved_document_id IS DISTINCT FROM p_document
   OR approval.resolved_artifact_id<>p_resolved_artifact OR approval.recipe_sha<>recipe.sha256 THEN
   RAISE EXCEPTION 'V2_APPROVAL_REPLAY_CONFLICT' USING ERRCODE='40001';
  END IF;
  RETURN jsonb_build_object('id',stream.id,'revision',stream.revision::text,
   'document_id',p_document,'approval_id',approval.id);
 END IF;
 IF stream.revision<>p_expected THEN
  RAISE EXCEPTION 'V2_APPROVAL_REVISION_CONFLICT' USING ERRCODE='40001';
 END IF;
 INSERT INTO photo.v2_document_approvals(tenant_id,project_id,document_id,actor_id,recipe_sha,resolved_artifact_id)
  VALUES(p_tenant,project,p_document,p_actor,recipe.sha256,p_resolved_artifact) RETURNING * INTO approval;
 UPDATE photo.v2_document_streams SET approved_document_id=p_document
  WHERE tenant_id=p_tenant AND id=stream.id RETURNING * INTO stream;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,
  resource_id,request_id,details)
 VALUES(p_tenant,'user',p_actor,'v2_draft.approved','v2_document',p_document,
  gen_random_uuid(),jsonb_build_object('approval_id',approval.id,
   'stream_id',stream.id,'asset_version_id',p_asset_version,
   'recipe_sha',recipe.sha256,'resolved_artifact_id',p_resolved_artifact));
 RETURN jsonb_build_object('id',stream.id,'revision',stream.revision::text,
  'document_id',p_document,'approval_id',approval.id);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.approve_v2_draft(uuid,uuid,bigint,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.approve_v2_draft(uuid,uuid,bigint,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.approve_v2_draft(uuid,uuid,bigint,uuid,uuid,uuid,uuid,text) TO photo_api;
