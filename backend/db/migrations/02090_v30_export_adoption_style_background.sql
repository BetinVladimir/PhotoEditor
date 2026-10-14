-- +goose Up
-- Resolve a frozen style background under the current scheduler claim. The
-- binary must be a sealed input and remain bound to the same accepted style.
-- Imported prepared assets retain their registered document provenance.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_style_background(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_style_version uuid,p_style uuid,p_style_sha text,p_portable text,
 p_source uuid,p_source_sha text,p_expected_bytes bigint,p_media text,p_bucket text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,
 bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_style_version IS NULL OR p_style IS NULL OR p_source IS NULL
  OR coalesce(p_style_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_portable IS NULL OR length(p_portable) NOT BETWEEN 1 AND 120
  OR p_portable<>btrim(p_portable)
  OR p_expected_bytes NOT BETWEEN 1 AND 536870912
  OR p_media NOT IN ('image/jpeg','image/png','image/tiff')
  OR coalesce(p_bucket,'')=''
 THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant
  AND id=p_workflow AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN;END IF;
 RETURN QUERY SELECT asset.bucket,asset.object_key,asset.object_version,
  asset.sha256,asset.bytes,asset.media_type
 FROM photo.styles style
 JOIN photo.style_versions version ON version.tenant_id=style.tenant_id
  AND version.style_id=style.id AND version.id=p_style_version
 JOIN photo.style_asset_bindings binding ON binding.tenant_id=version.tenant_id
  AND binding.style_version_id=version.id AND binding.portable_asset_id=p_portable
  AND binding.artifact_id=p_source AND binding.sha256=p_source_sha
 JOIN photo.artifacts asset ON asset.tenant_id=binding.tenant_id
  AND asset.id=binding.artifact_id AND asset.sha256=binding.sha256
 JOIN photo.artifacts config ON config.tenant_id=version.tenant_id
  AND config.id=version.export_artifact_id AND config.sha256=version.config_hash
 JOIN LATERAL photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_style,p_style_sha,'StyleV2') style_input
  ON style_input.sha256=config.sha256 AND style_input.bucket=config.bucket
  AND style_input.object_key=config.object_key
  AND style_input.object_version=config.object_version
  AND style_input.bytes=config.bytes AND style_input.media_type=config.media_type
 JOIN LATERAL photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_source,p_source_sha,'StyleBackgroundAsset') asset_input
  ON asset_input.sha256=asset.sha256 AND asset_input.bucket=asset.bucket
  AND asset_input.object_key=asset.object_key
  AND asset_input.object_version=asset.object_version
  AND asset_input.bytes=asset.bytes AND asset_input.media_type=asset.media_type
 WHERE style.tenant_id=p_tenant AND style.project_id=w.project_id
  AND style.archived_at IS NULL
  AND version.approval_status='accepted'
  AND version.export_artifact_id=p_style AND version.config_hash=p_style_sha
  AND version.config_json#>>'{extensions,background,mode}'='asset'
  AND version.config_json#>>'{extensions,background,asset_id}'=p_portable
  AND asset.bucket=p_bucket AND asset.bytes=p_expected_bytes
  AND asset.media_type=p_media AND asset.state='ready'
  AND asset.verified_at IS NOT NULL
  AND asset.object_version IS NOT NULL AND asset.object_version NOT IN ('','null')
  AND (asset.expires_at IS NULL OR asset.expires_at>clock_timestamp())
  AND config.state='ready' AND config.verified_at IS NOT NULL
  AND config.media_type='application/json' AND config.bytes BETWEEN 2 AND 1048576
  AND config.object_version IS NOT NULL AND config.object_version NOT IN ('','null')
  AND (config.expires_at IS NULL OR config.expires_at>clock_timestamp())
  AND (asset.output_role='style_asset' OR (
   asset.output_role='v30_import_prepared'
   AND p_portable=p_source::text
   AND version.metrics->>'schema_version'='interchange-style-approval/v1'
   AND EXISTS(SELECT 1 FROM photo.backend_documents document
    WHERE document.tenant_id=asset.tenant_id
     AND document.artifact_id=asset.id AND document.owner_id=asset.id
     AND document.purpose='v30_import_prepared' AND document.state='ready'
     AND document.sha256=asset.sha256 AND document.bytes=asset.bytes
     AND document.bucket=asset.bucket
     AND document.object_version=asset.object_version
     AND document.v30_import_prepared->>'contract'='StyleBackgroundAsset'
     AND document.v30_import_prepared->>'workflow'=version.metrics->>'workflow_id'
     AND document.v30_import_prepared->>'entry'=version.metrics->>'entry_id'
     AND document.v30_import_prepared#>'{background_occurrence,style}'=
      jsonb_build_object('artifact_id',version.metrics#>>'{original,artifact_id}',
       'sha256',version.metrics#>>'{original,sha256}'))))
 FOR SHARE OF style,version,binding,asset,config;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_style_background(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text,text,uuid,text,bigint,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_style_background(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text,text,uuid,text,bigint,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_style_background(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text,text,uuid,text,bigint,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
