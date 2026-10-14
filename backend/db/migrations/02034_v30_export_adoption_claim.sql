-- +goose Up
-- A scheduler claim is discovery metadata, not result/object/adoption authority.
-- The token is replaced on each retry after the cooldown; downstream readers
-- must recheck it before object IO and again in the adoption transaction.
-- +goose StatementBegin
ALTER TABLE photo.v2_workflows
 ADD COLUMN v30_export_adoption_retry_after timestamptz,
 ADD COLUMN v30_export_adoption_token uuid;
GRANT UPDATE(v30_export_adoption_retry_after,v30_export_adoption_token)
 ON photo.v2_workflows TO photo_fn_owner;
CREATE INDEX v2_workflows_v30_export_adoption_retry
 ON photo.v2_workflows(v30_export_adoption_retry_after,id)
 WHERE workflow_type='interchange';

CREATE FUNCTION photo.v30_claim_next_export_adoption()
RETURNS TABLE(tenant_id uuid,workflow_id uuid,run_id uuid,task_id uuid,
 result_id uuid,actor_id uuid,idempotency_key text,request_sha text,
 frozen_request jsonb,claim_token uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH candidate AS MATERIALIZED (
  SELECT w.tenant_id,w.id AS workflow_id,w.run_id,t.id AS task_id,
   z.id AS result_id,r.created_by AS actor_id,i.key AS idempotency_key,
   i.request_hash AS request_sha,i.response_body->'frozen'->'request' AS frozen_request
  FROM photo.v2_workflows w
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  JOIN photo.tenants tenant ON tenant.id=w.tenant_id AND tenant.status='active'
  JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id
   AND p.archived_at IS NULL AND p.download_allowed
  JOIN photo.idempotency_records i ON i.tenant_id=w.tenant_id
   AND i.principal_type='user' AND i.principal_id=r.created_by
   AND i.route='Interchange.Export.Pending' AND i.response_status IS NULL
   AND i.expires_at>clock_timestamp() AND length(i.key) BETWEEN 1 AND 120
   AND i.request_hash=r.config_snapshot->>'request_sha'
   AND photo.v30_uuid5(w.tenant_id,'interchange:create:'||r.created_by||':'||i.key)=w.run_id
  JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id
   AND t.id=photo.v30_uuid5(w.id,'v2.interchange.export:0')
   AND t.operation='v2.interchange.export' AND t.operation_version='1'
   AND t.state='succeeded' AND NOT t.cancel_requested
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
   AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  WHERE w.workflow_type='interchange' AND r.kind='v2' AND r.status='running'
   AND r.graph_sealed AND NOT r.cancel_requested
   AND w.renderer_version='v2.2' AND w.policy_sha=i.request_hash
   AND (w.v30_export_adoption_retry_after IS NULL
    OR w.v30_export_adoption_retry_after<=clock_timestamp())
   AND jsonb_typeof(i.response_body->'frozen'->'request')='object'
   AND i.response_body->'frozen'->'request'->>'direction'='export'
   AND i.response_body->'frozen'->'request'->>'project_id'=w.project_id::text
   AND i.response_body->'frozen'->'request'->>'snapshot_id'=w.input_snapshot_id::text
   AND i.response_body->'frozen'->'request'->>'format'
    IN ('photoeditor_bundle','xmp','captureone_sidecar')
   AND NOT EXISTS(SELECT 1 FROM photo.inbox receipt
    WHERE receipt.tenant_id=w.tenant_id AND receipt.consumer='v30-interchange-export'
     AND receipt.event_id=photo.v2_adoption_event_id(t.id,z.id))
  ORDER BY coalesce(w.v30_export_adoption_retry_after,'-infinity'::timestamptz),
   r.created_at,w.id LIMIT 1 FOR UPDATE OF w SKIP LOCKED
 ), claimed AS (
  UPDATE photo.v2_workflows w
  SET v30_export_adoption_retry_after=clock_timestamp()+interval '31 minutes',
   v30_export_adoption_token=gen_random_uuid()
  FROM candidate c WHERE w.tenant_id=c.tenant_id AND w.id=c.workflow_id
  RETURNING w.tenant_id,w.id,w.v30_export_adoption_token
 )
 SELECT c.*,claimed.v30_export_adoption_token FROM candidate c JOIN claimed
  ON claimed.tenant_id=c.tenant_id AND claimed.id=c.workflow_id
$$;
ALTER FUNCTION photo.v30_claim_next_export_adoption() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_claim_next_export_adoption()
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_claim_next_export_adoption() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
