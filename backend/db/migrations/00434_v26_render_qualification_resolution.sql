-- +goose Up
-- Resolve only the selected trusted policy, never client pins or latest reports.
-- +goose StatementBegin
CREATE FUNCTION photo.resolve_v26_render_qualification(
 p_tenant uuid,p_project uuid)
RETURNS TABLE(model_lock_sha text,runtime_lock_sha text,backend text,device_profile_sha text,dataset_sha text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT policy.model_lock_sha,policy.runtime_lock_sha,policy.backend,policy.device_profile_sha,policy.dataset_sha
 FROM photo.v2_qualification_policies policy
 WHERE p_tenant IS NOT NULL AND p_project IS NOT NULL
  AND nullif(current_setting('app.tenant_id',true),'')::uuid=p_tenant
  AND policy.operation='render.compose' AND policy.operation_version='1'
  AND EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=policy.operation
   AND op.version=policy.operation_version AND op.v2_enabled AND op.backends ? policy.backend)
  AND EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=p_tenant AND tenant.status='active')
  AND policy.admission_enabled AND policy.enabled AND policy.license_cleared
  AND EXISTS(SELECT 1 FROM photo.projects project
   WHERE project.tenant_id=p_tenant AND project.id=p_project AND project.archived_at IS NULL)
  AND EXISTS(
   SELECT 1 FROM photo.model_qualifications qualified
   JOIN photo.artifacts report ON report.tenant_id=qualified.tenant_id
    AND report.id=qualified.report_artifact_id
    AND report.sha256=qualified.report_sha
   WHERE qualified.tenant_id=p_tenant AND qualified.project_id=p_project
    AND qualified.operation=policy.operation
    AND qualified.operation_version=policy.operation_version
    AND qualified.model_lock_sha=policy.model_lock_sha
    AND qualified.runtime_lock_sha=policy.runtime_lock_sha
    AND qualified.backend=policy.backend
    AND qualified.device_profile_sha=policy.device_profile_sha
    AND qualified.dataset_sha=policy.dataset_sha
    AND qualified.trusted_import AND qualified.status='passed'
    AND report.state='ready' AND report.media_type='application/json'
    AND report.verified_at IS NOT NULL AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
    AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp()))
$$;

-- +goose StatementEnd
ALTER FUNCTION photo.resolve_v26_render_qualification(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.resolve_v26_render_qualification(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.resolve_v26_render_qualification(uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
