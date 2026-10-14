-- +goose Up
-- Ready-only historical transport lookup. No reservations, quota or state writes.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v27_native_ready_transport(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_actor uuid,p_session uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.projects project ON project.tenant_id=w.tenant_id AND project.id=w.project_id AND project.archived_at IS NULL
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id=p_owner
 JOIN photo.backend_documents b ON b.tenant_id=t.tenant_id AND b.run_id=t.run_id AND b.owner_id=t.id AND b.purpose=p_purpose
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 JOIN photo.authorize_tenant(p_actor,p_session,p_tenant) authority ON authority.role IN('owner','admin','editor')
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND r.graph_sealed AND r.created_by=p_actor
 AND r.config_snapshot->'v27_semantic'->>'auth_session_id'=p_session::text
 AND w.workflow_type='semantic' AND w.renderer_version='v2.2'
 AND (project.visibility<>'restricted' OR authority.role IN('owner','admin') OR EXISTS(SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=project.id AND acl.user_id=p_actor AND acl.role IN('owner','admin','editor')))
 AND t.operation='semantic.transfer' AND t.operation_version='1'
 AND (p_purpose='v2_input_manifest' OR CASE WHEN p_purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$' THEN substring(p_purpose from 16)::integer BETWEEN 0 AND 1953 ELSE false END)
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.task_id=p_owner AND input.project_id=project.id AND input.artifact_id=a.id AND input.sha256=a.sha256 AND input.contract=CASE WHEN p_purpose='v2_input_manifest' THEN 'InputClosureManifest' ELSE 'InputClosureChunk' END)
 AND b.state='ready' AND b.sha256=p_sha AND b.bytes=p_bytes AND b.bucket=p_bucket
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=b.sha256 AND a.bytes=b.bytes
 AND a.bucket=b.bucket AND a.object_version=b.object_version AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
$$;
ALTER FUNCTION photo.read_v27_native_ready_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v27_native_ready_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v27_native_ready_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
