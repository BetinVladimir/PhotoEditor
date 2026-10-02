-- +goose Up
-- Expose only the immutable InputSnapshot object of an analysis that the
-- authenticated editor may decide. Backend documents are not table-readable
-- by the API role.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_apply_input_source(
 p_tenant uuid,p_analysis uuid,p_actor uuid,p_auth_session uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.session_analyses analysis
 JOIN photo.v2_workflows w ON w.tenant_id=analysis.tenant_id
  AND w.id=analysis.workflow_id AND w.project_id=analysis.project_id
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.backend_documents d ON d.tenant_id=r.tenant_id
  AND d.run_id=r.id AND d.owner_id=r.id
  AND d.purpose='v2_analysis_input_snapshot' AND d.state='ready'
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  AND a.sha256=d.sha256 AND a.object_version=d.object_version
 JOIN photo.tenants tenant ON tenant.id=analysis.tenant_id
 WHERE analysis.tenant_id=p_tenant AND analysis.id=p_analysis
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND w.input_snapshot_id=analysis.snapshot_id
  AND r.kind='v2' AND r.status IN('waiting_review','completed')
  AND NOT r.cancel_requested AND tenant.status='active'
  AND photo.v2_editor_authorized(p_tenant,analysis.project_id,p_actor,p_auth_session)
  AND a.state='ready' AND a.media_type='application/json'
  AND a.bytes BETWEEN 2 AND 8388608
  AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
  AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_apply_input_source(uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_apply_input_source(uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_analysis_apply_input_source(uuid,uuid,uuid,uuid)
 TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
