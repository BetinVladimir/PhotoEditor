-- +goose Up
-- Caller-authorized technical quality review; no approval or version mutation.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_quality_review(p_tenant uuid,p_project uuid,p_operation text,
 p_task uuid,p_actor uuid,p_session uuid,p_result uuid,p_root_sha text)
RETURNS TABLE(quality_run_id uuid,quality_task_id uuid,quality_result_id uuid,report_id uuid,report_sha text,report text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH candidates AS (
 SELECT r.*,b.artifact_id AS root_id FROM photo.v29_adopted_multi_binding(p_tenant,p_project,p_task,p_actor,p_session,p_operation) b
 JOIN photo.runs r ON r.tenant_id=p_tenant AND r.v29_multi_quality_task_id=p_task
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid AND b.result_id=p_result AND b.sha256=p_root_sha
  AND r.kind='preview' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.config_snapshot->>'project_id'=p_project::text AND r.config_snapshot->>'operation'=p_operation
  AND EXISTS(SELECT 1 FROM photo.tasks producer WHERE producer.tenant_id=p_tenant AND producer.id=p_task
   AND producer.attempt::text=r.config_snapshot->>'source_attempt' AND producer.fencing_token::text=r.config_snapshot->>'source_fence')
  AND r.config_snapshot->>'source_result_id'=b.result_id::text AND r.config_snapshot->>'threshold_profile_id'='master_v1' AND r.config_snapshot->>'stage'='master'
  AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',b.artifact_id,'sha256',b.sha256)
  AND (SELECT count(*) FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=r.id
   AND d.purpose='task_params' AND d.state='ready' AND d.v29_multi_quality_receipt IS NOT NULL)=1
 ), current_receipts AS (
 SELECT r.id,r.created_at,t.id AS task_id,t.attempt,t.fencing_token,t.canonical_result_id,d.v29_multi_quality_receipt AS receipt
 FROM candidates r JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=r.id
 JOIN photo.backend_documents d ON d.tenant_id=p_tenant AND d.run_id=r.id AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 WHERE t.state='succeeded' AND NOT t.cancel_requested AND t.operation='quality.check' AND t.operation_version='1'
  AND t.node_key='quality.check:master' AND d.v29_multi_quality_params::jsonb->>'threshold_profile_id'='master_v1'
  AND d.v29_multi_quality_receipt->>'result_id'=t.canonical_result_id::text
  AND d.v29_multi_quality_receipt->>'attempt'=t.attempt::text
  AND d.v29_multi_quality_receipt->>'fence'=t.fencing_token::text
  AND jsonb_array_length(d.v29_multi_quality_receipt->'locations')=2
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(d.v29_multi_quality_receipt->'locations') w
   LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id::text=w->>'artifact_id'
   WHERE a.id IS NULL OR a.sha256 IS DISTINCT FROM w->>'sha256' OR a.bucket IS DISTINCT FROM w->>'bucket'
    OR a.object_key IS DISTINCT FROM w->>'key' OR a.object_version IS DISTINCT FROM w->>'version'
    OR to_jsonb(a.bytes) IS DISTINCT FROM w->'bytes' OR a.media_type IS DISTINCT FROM w->>'media_type'
    OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
    OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))
 )
 SELECT c.id,c.task_id,c.canonical_result_id,a.id,a.sha256,c.receipt->>'manifest'
 FROM current_receipts c CROSS JOIN LATERAL jsonb_array_elements(c.receipt->'objects') o
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id::text=o->>'artifact_id' AND a.output_role='quality_report'
 JOIN photo.task_results z ON z.tenant_id=p_tenant AND z.id=c.canonical_result_id AND z.task_id=c.task_id
 JOIN photo.output_uploads u ON u.tenant_id=p_tenant AND u.task_id=c.task_id AND u.artifact_id=a.id
 WHERE o->>'role'='quality_report' AND a.sha256=o->>'sha256' AND a.bucket=o->>'bucket'
  AND a.object_key=o->>'key' AND a.object_version=o->>'version' AND a.bytes=(o->>'bytes')::bigint AND a.media_type=o->>'media_type'
  AND a.created_by_task_id=c.task_id AND a.produced_attempt=c.attempt AND a.produced_fence=c.fencing_token
  AND z.attempt=c.attempt AND z.fencing_token=c.fencing_token
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
  AND u.state='ready' AND u.attempt=c.attempt AND u.fence=c.fencing_token AND u.role='quality_report'
  AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version
  AND u.sha256=a.sha256 AND u.bytes=a.bytes AND u.media_type=a.media_type
 ORDER BY c.created_at DESC,c.id LIMIT 1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v29_multi_quality_review(uuid,uuid,text,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_quality_review(uuid,uuid,text,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_multi_quality_review(uuid,uuid,text,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
