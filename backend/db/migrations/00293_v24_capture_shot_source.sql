-- +goose Up
-- Read the frozen document, not the mutable current shot-list pointer.
-- +goose StatementBegin
CREATE FUNCTION photo.capture_shot_source(p_tenant uuid,p_job uuid,p_token uuid)
RETURNS TABLE(document_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT d.id,d.artifact_id,d.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.capture_assessment_jobs job JOIN photo.tenants tenant ON tenant.id=job.tenant_id AND tenant.status='active'
 JOIN photo.v2_documents d ON d.tenant_id=job.tenant_id AND d.project_id=job.project_id
 AND d.id=(job.selection_seed->'shot_document'->>'document_id')::uuid
 AND d.artifact_id=(job.selection_seed->'shot_document'->>'artifact_id')::uuid
 AND d.sha256=job.selection_seed->'shot_document'->>'sha256'
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
 WHERE job.tenant_id=p_tenant AND job.id=p_job AND job.state='leased' AND job.lease_token=p_token
 AND job.lease_until>clock_timestamp() AND d.document_type='ShotList' AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_shot_source(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_shot_source(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_shot_source(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
