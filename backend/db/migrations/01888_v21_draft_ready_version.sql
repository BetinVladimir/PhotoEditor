-- +goose Up
-- A ready reservation is a catalogue claim. Replay must read its exact object
-- version before returning the existing reference to the editor.
-- +goose StatementBegin
CREATE FUNCTION photo.v21_ready_draft_object(
 p_tenant uuid,p_document uuid,p_actor uuid,p_session uuid,p_artifact uuid,p_sha text,p_bucket text)
RETURNS TABLE(object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_document IS NULL OR p_actor IS NULL OR p_session IS NULL
  OR p_artifact IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bucket IS NULL
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR NOT EXISTS(SELECT FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active') THEN
  RETURN;
 END IF;
 RETURN QUERY SELECT a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v2_draft_uploads d
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.document_id=p_document AND d.artifact_id=p_artifact
  AND d.sha256=p_sha AND d.bucket=p_bucket AND d.state='ready'
  AND d.object_version IS NOT NULL AND d.object_version NOT IN('','null')
  AND photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session)
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document'
  AND a.bucket=d.bucket AND a.sha256=d.sha256 AND a.bytes=d.bytes
  AND a.media_type='application/json' AND a.object_version=d.object_version
  AND a.object_key='tenants/'||p_tenant::text||'/v2-drafts/'||p_artifact::text
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v21_ready_draft_object(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v21_ready_draft_object(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v21_ready_draft_object(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
