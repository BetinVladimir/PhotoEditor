-- +goose Up
-- Only the adopted archive is downloadable; generic task output presence is insufficient.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_export_download(p_actor uuid,p_session uuid,p_tenant uuid,p_run uuid)
RETURNS TABLE(artifact_id uuid,output_role text,sha256 text,bytes bigint,media_type text,bucket text,object_key text,object_version text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.output_role::text,a.sha256,a.bytes,a.media_type,a.bucket,a.object_key,a.object_version
 FROM photo.v2_workflows w
 CROSS JOIN LATERAL (SELECT photo.get_v30_interchange(p_tenant,p_actor,p_session,w.id) resource) scoped
 JOIN photo.v30_interchange_export_evidence e ON e.tenant_id=w.tenant_id AND e.run_id=w.run_id
 JOIN photo.artifacts a ON a.tenant_id=e.tenant_id AND a.id=(e.evidence_jcs::jsonb#>>'{bundle,artifact_id}')::uuid
 AND a.sha256=(e.evidence_jcs::jsonb#>>'{bundle,sha256}')
 WHERE w.tenant_id=p_tenant AND w.run_id=p_run
 AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND scoped.resource#>>'{data,direction}'='export'
 AND scoped.resource#>>'{data,bundle_artifact_id}'=a.id::text
 AND photo.v30_interchange_export_committed(p_tenant,p_run)
 AND a.output_role='bundle' AND a.media_type='application/zip'
 AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
$$;
ALTER FUNCTION photo.v30_interchange_export_download(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_export_download(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_export_download(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
