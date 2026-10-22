-- +goose Up
-- Scheduler needs only hashes/sizes of this exact leased import's package or
-- manifest. It must not receive table SELECT or arbitrary object locators.
-- +goose StatementBegin
CREATE FUNCTION photo.style_import_artifact_ref(p_tenant uuid,p_run uuid,p_task uuid,p_token uuid,p_artifact uuid)
RETURNS TABLE(id uuid,sha256 text,bytes bigint)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bytes FROM photo.style_import_builds b
 JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=p_artifact
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.task_id=p_task
 AND b.lease_token=p_token AND b.state='building' AND b.lease_until>=clock_timestamp()
 AND r.kind='style_import' AND r.status IN('queued','running') AND NOT r.cancel_requested
 AND a.id IN(b.package_artifact_id,b.manifest_artifact_id)
 AND a.state='ready' AND a.object_version IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
$$;
ALTER FUNCTION photo.style_import_artifact_ref(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.style_import_artifact_ref(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.style_import_artifact_ref(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02648 is forward-only';END $$;
-- +goose StatementEnd
