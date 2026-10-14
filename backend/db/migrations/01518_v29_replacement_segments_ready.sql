-- +goose Up
-- Ready segment observation for final auto-perspective admission; no publication.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_replacement_segments_ready(p_tenant uuid,p_run uuid,p_owner uuid,p_sha text,p_bytes bigint,p_bucket text,p_proof jsonb)
RETURNS TABLE(artifact_id uuid,object_key text,object_version text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
 OR photo.v29_replacement_segments_document_scope(p_tenant,p_run,p_owner,'task_params',p_sha,p_bytes,p_proof) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.object_key,a.object_version
 FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose='task_params'
 AND b.state='ready' AND b.artifact_id=photo.v30_uuid5(p_owner,'params')
 AND b.sha256=p_sha AND b.bytes=p_bytes AND b.bucket=p_bucket
 AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.state='ready' AND a.media_type='application/json' AND a.output_role='task_params'
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||a.id
 AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
END $$;
ALTER FUNCTION photo.read_v29_replacement_segments_ready(uuid,uuid,uuid,text,bigint,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_replacement_segments_ready(uuid,uuid,uuid,text,bigint,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_replacement_segments_ready(uuid,uuid,uuid,text,bigint,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
