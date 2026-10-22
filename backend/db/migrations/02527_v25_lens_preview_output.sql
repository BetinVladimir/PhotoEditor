-- +goose Up
-- Caller-authorized, current-result and encoded-receipt-bound preview only.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_preview_output(p_tenant uuid,p_project uuid,p_version uuid,
 p_task uuid,p_actor uuid,p_session uuid,p_result uuid,p_root_sha text,p_srgb text,p_variant text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,media_type text,bucket text,object_key text,object_version text,width integer,height integer)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH candidates AS (
 SELECT r.*,b.artifact_id AS root_id FROM photo.v25_adopted_lens_binding(p_tenant,p_project,p_version,p_task,p_actor,p_session) b
 JOIN photo.runs r ON r.tenant_id=p_tenant AND r.v25_lens_preview_task_id=p_task
 WHERE b.result_id=p_result AND b.sha256=p_root_sha AND p_srgb ~ '^[0-9a-f]{64}$'
  AND p_variant IN('preview1600','preview2560') AND r.kind='preview' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.config_snapshot->>'project_id'=p_project::text AND r.config_snapshot->'base'->>'asset_version_id'=p_version::text
  AND r.config_snapshot->>'source_result_id'=b.result_id::text AND r.config_snapshot->>'srgb_sha256'=p_srgb
  AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',b.artifact_id,'sha256',b.sha256)
  AND (SELECT count(*) FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=r.id
   AND d.purpose='task_params' AND d.state='ready' AND d.v25_lens_preview_receipt IS NOT NULL)=2
 ), current_receipts AS (
 SELECT r.id,r.created_at,t.id AS task_id,t.attempt,t.fencing_token,t.canonical_result_id,d.v25_lens_preview_receipt AS receipt
 FROM candidates r JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=r.id
 JOIN photo.backend_documents d ON d.tenant_id=p_tenant AND d.run_id=r.id AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 WHERE t.state='succeeded' AND NOT t.cancel_requested AND t.operation='media.preview' AND t.operation_version='1'
  AND t.node_key='render.derivative:'||p_variant AND d.v25_lens_preview_params::jsonb->>'variant'=p_variant
  AND d.v25_lens_preview_receipt->>'result_id'=t.canonical_result_id::text
  AND d.v25_lens_preview_receipt->>'attempt'=t.attempt::text
  AND d.v25_lens_preview_receipt->>'fence'=t.fencing_token::text
  AND jsonb_array_length(d.v25_lens_preview_receipt->'locations')=4
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(d.v25_lens_preview_receipt->'locations') w
   LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id::text=w->>'artifact_id'
   WHERE a.id IS NULL OR a.sha256 IS DISTINCT FROM w->>'sha256' OR a.bucket IS DISTINCT FROM w->>'bucket'
    OR a.object_key IS DISTINCT FROM w->>'key' OR a.object_version IS DISTINCT FROM w->>'version'
    OR to_jsonb(a.bytes) IS DISTINCT FROM w->'bytes' OR a.media_type IS DISTINCT FROM w->>'media_type'
    OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
    OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))
 )
 SELECT a.id,a.sha256,a.bytes,a.media_type,a.bucket,a.object_key,a.object_version,
  ((c.receipt->>'manifest')::jsonb->>'width')::integer, ((c.receipt->>'manifest')::jsonb->>'height')::integer
 FROM current_receipts c CROSS JOIN LATERAL jsonb_array_elements(c.receipt->'objects') o
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id::text=o->>'artifact_id' AND a.output_role='derivative'
 JOIN photo.task_results z ON z.tenant_id=p_tenant AND z.id=c.canonical_result_id AND z.task_id=c.task_id
 JOIN photo.output_uploads u ON u.tenant_id=p_tenant AND u.task_id=c.task_id AND u.artifact_id=a.id
 WHERE o->>'role'='derivative' AND a.sha256=o->>'sha256' AND a.bucket=o->>'bucket'
  AND a.object_key=o->>'key' AND a.object_version=o->>'version' AND a.bytes=(o->>'bytes')::bigint AND a.media_type=o->>'media_type'
  AND a.created_by_task_id=c.task_id AND a.produced_attempt=c.attempt AND a.produced_fence=c.fencing_token
  AND z.attempt=c.attempt AND z.fencing_token=c.fencing_token
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
  AND u.state='ready' AND u.attempt=c.attempt AND u.fence=c.fencing_token AND u.role='derivative'
  AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version
  AND u.sha256=a.sha256 AND u.bytes=a.bytes AND u.media_type=a.media_type
 ORDER BY c.created_at DESC,c.id LIMIT 1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_preview_output(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_preview_output(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v25_lens_preview_output(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
