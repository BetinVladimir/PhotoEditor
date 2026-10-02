-- Server stages/validates recipe bytes and closure before this DB-only transaction.
-- First save includes a server-generated base checkpoint; later saves omit it.
-- +goose StatementBegin
CREATE FUNCTION photo.replace_v2_draft(
 p_tenant uuid,p_asset_version uuid,p_expected bigint,p_document uuid,p_artifact uuid,p_sha text,
 p_actor uuid,p_session uuid,p_refs jsonb,p_base_document uuid,p_base_artifact uuid,
 p_base_sha text,p_base_refs jsonb,p_layer_document uuid,p_layer_artifact uuid,
 p_layer_sha text,p_layer_refs jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid; asset uuid; latest_version uuid;
 stream photo.v2_document_streams%ROWTYPE; next_version bigint;
BEGIN
 IF p_tenant IS NULL OR p_asset_version IS NULL OR p_document IS NULL OR p_artifact IS NULL
  OR p_actor IS NULL OR p_session IS NULL OR p_expected IS NULL OR p_expected<0
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V2_DRAFT_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V2_TENANT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 SELECT a.project_id,a.id INTO project,asset FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 WHERE v.tenant_id=p_tenant AND v.id=p_asset_version AND v.review_state='accepted'
  AND a.deleted_at IS NULL AND a.upload_state='verified'
  FOR UPDATE OF a FOR SHARE OF v;
 IF project IS NULL OR NOT photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) THEN
  RAISE EXCEPTION 'V2_DRAFT_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT id INTO latest_version FROM photo.asset_versions
  WHERE tenant_id=p_tenant AND asset_id=asset ORDER BY version_no DESC LIMIT 1;
 IF latest_version IS DISTINCT FROM p_asset_version THEN
  RAISE EXCEPTION 'V2_DRAFT_SOURCE_STALE' USING ERRCODE='40001';
 END IF;
 SELECT * INTO stream FROM photo.v2_document_streams
  WHERE tenant_id=p_tenant AND asset_version_id=p_asset_version AND stream_type='draft' FOR UPDATE;
 IF NOT FOUND THEN
  IF p_expected<>0 OR p_base_document IS NULL OR p_base_artifact IS NULL
   OR coalesce(p_base_sha,'') !~ '^[0-9a-f]{64}$' OR p_base_refs IS NULL
   OR p_base_document=p_document THEN
   RAISE EXCEPTION 'V2_DRAFT_REVISION_CONFLICT' USING ERRCODE='40001';
  END IF;
  INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type)
   VALUES(p_tenant,project,p_asset_version,'draft') ON CONFLICT DO NOTHING;
  SELECT * INTO stream FROM photo.v2_document_streams
   WHERE tenant_id=p_tenant AND asset_version_id=p_asset_version AND stream_type='draft' FOR UPDATE;
  IF NOT FOUND OR stream.revision<>1 OR stream.current_document_id IS NOT NULL THEN
   RAISE EXCEPTION 'V2_DRAFT_REVISION_CONFLICT' USING ERRCODE='40001';
  END IF;
  PERFORM 1 FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant
   AND u.project_id=project AND u.asset_version_id=p_asset_version
   AND u.document_id=p_base_document AND u.artifact_id=p_base_artifact
   AND u.sha256=p_base_sha AND u.state='ready' FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V2_DRAFT_BASE_NOT_READY' USING ERRCODE='23514';END IF;
  INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256)
   VALUES(p_base_document,p_tenant,project,stream.id,1,'EditingRecipeV3',p_base_artifact,p_base_sha);
  PERFORM photo.register_v2_document_refs(p_tenant,project,p_base_document,p_base_refs);
  next_version:=2;
 ELSE
  IF stream.project_id<>project OR stream.revision<>p_expected OR p_expected<1
   OR p_base_document IS NOT NULL OR p_base_artifact IS NOT NULL OR p_base_sha IS NOT NULL
   OR p_base_refs IS DISTINCT FROM '[]'::jsonb THEN
   RAISE EXCEPTION 'V2_DRAFT_REVISION_CONFLICT' USING ERRCODE='40001';
  END IF;
  SELECT max(version_no)+1 INTO next_version FROM photo.v2_documents
   WHERE tenant_id=p_tenant AND stream_id=stream.id;
  IF next_version IS NULL THEN
   RAISE EXCEPTION 'V2_DRAFT_HISTORY_INVALID' USING ERRCODE='23514';
  END IF;
 END IF;
 PERFORM 1 FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant
  AND u.project_id=project AND u.asset_version_id=p_asset_version
  AND u.document_id=p_document AND u.artifact_id=p_artifact
  AND u.sha256=p_sha AND u.state='ready' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V2_DRAFT_UPLOAD_NOT_READY' USING ERRCODE='23514';END IF;
 IF p_layer_document IS NOT NULL THEN
  IF p_layer_artifact IS NULL OR p_layer_document=p_document
   OR p_layer_document IS NOT DISTINCT FROM p_base_document
   OR coalesce(p_layer_sha,'') !~ '^[0-9a-f]{64}$' OR p_layer_refs IS NULL
   OR p_refs IS NULL OR jsonb_typeof(p_refs)<>'array' OR NOT EXISTS(
    SELECT 1 FROM jsonb_array_elements(p_refs) x
    WHERE x->>'artifact_id'=p_layer_artifact::text AND x->>'sha256'=p_layer_sha) THEN
   RAISE EXCEPTION 'V2_DRAFT_LAYER_INVALID' USING ERRCODE='22023';
  END IF;
 ELSIF p_layer_artifact IS NOT NULL OR p_layer_sha IS NOT NULL OR p_layer_refs IS NOT NULL THEN
  RAISE EXCEPTION 'V2_DRAFT_LAYER_INVALID' USING ERRCODE='22023';
 END IF;
 IF p_layer_document IS NOT NULL THEN
  PERFORM 1 FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant
   AND u.project_id=project AND u.asset_version_id=p_asset_version
   AND u.document_id=p_layer_document AND u.artifact_id=p_layer_artifact
   AND u.sha256=p_layer_sha AND u.state='ready' FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V2_DRAFT_LAYER_NOT_READY' USING ERRCODE='23514';END IF;
  INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256)
   VALUES(p_layer_document,p_tenant,project,stream.id,next_version,'LayerStack',p_layer_artifact,p_layer_sha);
  PERFORM photo.register_v2_document_refs(p_tenant,project,p_layer_document,p_layer_refs);
  next_version:=next_version+1;
 END IF;
 INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256)
  VALUES(p_document,p_tenant,project,stream.id,next_version,'EditingRecipeV3',p_artifact,p_sha);
 PERFORM photo.register_v2_document_refs(p_tenant,project,p_document,p_refs);
 UPDATE photo.v2_document_streams SET current_document_id=p_document
  WHERE tenant_id=p_tenant AND id=stream.id RETURNING * INTO stream;
 RETURN jsonb_build_object('id',stream.id,'revision',stream.revision::text,'document_id',p_document);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,jsonb,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,jsonb,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,jsonb,uuid,uuid,text,jsonb) TO photo_api;

-- Preserve the layer-free call shape for existing clients.
-- +goose StatementBegin
CREATE FUNCTION photo.replace_v2_draft(p_tenant uuid,p_asset_version uuid,p_expected bigint,
 p_document uuid,p_artifact uuid,p_sha text,p_actor uuid,p_session uuid,p_refs jsonb,
 p_base_document uuid,p_base_artifact uuid,p_base_sha text,p_base_refs jsonb) RETURNS jsonb
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.replace_v2_draft(p_tenant,p_asset_version,p_expected,p_document,
  p_artifact,p_sha,p_actor,p_session,p_refs,p_base_document,p_base_artifact,
  p_base_sha,p_base_refs,NULL::uuid,NULL::uuid,NULL::text,NULL::jsonb)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,jsonb) TO photo_api;
