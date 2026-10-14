-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_registered_style_background(p_tenant uuid,p_project uuid,p_version uuid,p_artifact uuid,p_sha text,p_portable text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.styles s JOIN photo.style_versions v ON v.tenant_id=s.tenant_id AND v.style_id=s.id
 JOIN photo.style_asset_bindings b ON b.tenant_id=v.tenant_id AND b.style_version_id=v.id
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id AND a.sha256=b.sha256
 JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id AND d.owner_id=a.id
 WHERE s.tenant_id=p_tenant AND s.project_id=p_project AND s.archived_at IS NULL
 AND v.id=p_version AND v.approval_status='accepted' AND v.metrics->>'schema_version'='interchange-style-approval/v1'
 AND b.portable_asset_id=p_portable AND b.artifact_id=p_artifact AND b.sha256=p_sha
 AND p_portable=p_artifact::text AND a.output_role='v30_import_prepared'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.bytes BETWEEN 8 AND 536870912 AND a.media_type IN('image/jpeg','image/png','image/tiff')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND d.purpose='v30_import_prepared' AND d.state='ready' AND d.sha256=a.sha256 AND d.bytes=a.bytes
 AND d.bucket=a.bucket AND d.object_version=a.object_version
 AND d.v30_import_prepared->>'contract'='StyleBackgroundAsset'
 AND d.v30_import_prepared->>'workflow'=v.metrics->>'workflow_id'
 AND d.v30_import_prepared->>'entry'=v.metrics->>'entry_id'
 AND d.v30_import_prepared#>'{background_occurrence,style}'=jsonb_build_object(
 'artifact_id',v.metrics#>>'{original,artifact_id}','sha256',v.metrics#>>'{original,sha256}')
 AND v.config_json#>>'{extensions,background,mode}'='asset'
 AND v.config_json#>>'{extensions,background,asset_id}'=p_portable);
END $$;
ALTER FUNCTION photo.v30_registered_style_background(uuid,uuid,uuid,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_registered_style_background(uuid,uuid,uuid,uuid,text,text,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_registered_style_background(uuid,uuid,uuid,uuid,text,text,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
