-- Exact encoded background access under an active Evaluate expansion lease.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_background_source(p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,
 p_version uuid,p_style uuid,p_style_sha text,p_source uuid,p_source_sha text,p_portable text,p_bytes bigint,p_mime text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.graph_expansions e
 JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tasks fit ON fit.tenant_id=e.tenant_id AND fit.run_id=e.run_id AND fit.id=e.parent_task_id
 JOIN photo.v2_task_inputs source_input ON source_input.tenant_id=fit.tenant_id AND source_input.task_id=fit.id
  AND source_input.artifact_id=p_source AND source_input.sha256=p_source_sha AND source_input.contract='StyleBackgroundAsset'
 JOIN photo.v2_task_inputs style_input ON style_input.tenant_id=fit.tenant_id AND style_input.task_id=fit.id
  AND style_input.artifact_id=p_style AND style_input.sha256=p_style_sha AND style_input.contract='StyleV2'
 JOIN photo.styles s ON s.tenant_id=w.tenant_id AND s.project_id=w.project_id AND s.archived_at IS NULL
 JOIN photo.style_versions v ON v.tenant_id=s.tenant_id AND v.style_id=s.id AND v.id=p_version
  AND v.export_artifact_id=p_style AND v.config_hash=p_style_sha AND v.approval_status='accepted'
 JOIN photo.style_asset_bindings b ON b.tenant_id=v.tenant_id AND b.style_version_id=v.id
  AND b.portable_asset_id=p_portable AND b.artifact_id=p_source AND b.sha256=p_source_sha
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id AND a.sha256=b.sha256
 JOIN photo.artifacts config ON config.tenant_id=v.tenant_id AND config.id=v.export_artifact_id AND config.sha256=v.config_hash
 WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion
 AND e.kind='v2.learning.evaluate/1' AND e.state='open' AND e.expansion_token=p_token
 AND e.expansion_lease_until>clock_timestamp() AND r.kind='v2' AND r.status='running'
 AND NOT r.graph_sealed AND NOT r.cancel_requested AND w.workflow_type='learning' AND w.renderer_version='v2.2'
 AND fit.operation='learning.fit' AND fit.operation_version='1' AND fit.state='succeeded' AND fit.canonical_result_id IS NOT NULL
 AND v.schema_version='2.0.0' AND v.renderer_version='photo_renderer_v2'
 AND v.config_json#>>'{extensions,background,mode}'='asset' AND v.config_json#>>'{extensions,background,asset_id}'=p_portable
 AND a.bytes=p_bytes AND p_bytes BETWEEN 1 AND 536870912 AND a.media_type=p_mime AND p_mime IN('image/jpeg','image/png','image/tiff')
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND config.state='ready' AND config.verified_at IS NOT NULL AND config.object_version NOT IN('','null')
 AND (config.expires_at IS NULL OR config.expires_at>clock_timestamp())
 AND (a.output_role='style_asset' OR (a.output_role='v30_import_prepared' AND
 photo.v23_registered_imported_background(p_tenant,w.project_id,v.id,a.id,a.sha256,p_portable)));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_background_source(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_background_source(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_background_source(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,bigint,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
