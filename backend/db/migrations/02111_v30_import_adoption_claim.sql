-- +goose Up
-- A discovery lease is distinct from authority to read or adopt a result.
ALTER TABLE photo.v2_workflows
 ADD COLUMN v30_import_adoption_retry_after timestamptz,
 ADD COLUMN v30_import_adoption_token uuid;
GRANT UPDATE(v30_import_adoption_retry_after,v30_import_adoption_token)
 ON photo.v2_workflows TO photo_fn_owner;
CREATE INDEX v2_workflows_v30_import_adoption_retry
 ON photo.v2_workflows(v30_import_adoption_retry_after,id)
 WHERE workflow_type='interchange';

-- +goose StatementBegin
CREATE FUNCTION photo.v30_claim_next_import_adoption()
RETURNS TABLE(tenant_id uuid,workflow_id uuid,run_id uuid,task_id uuid,
 result_id uuid,attempt integer,fencing_token bigint,claim_token uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH candidate AS MATERIALIZED (
  SELECT w.tenant_id,w.id AS workflow_id,w.run_id,t.id AS task_id,
   z.id AS result_id,t.attempt,t.fencing_token
  FROM photo.v2_workflows w
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  JOIN photo.tenants tenant ON tenant.id=w.tenant_id AND tenant.status='active'
  JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id
   AND t.id=photo.v30_uuid5(w.id,'v2.interchange.import:0')
   AND t.operation='v2.interchange.import' AND t.operation_version='1'
   AND t.resource_class='cpu' AND t.state='succeeded'
   AND NOT t.cancel_requested
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
   AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  WHERE w.workflow_type='interchange' AND w.renderer_version='v2.2'
   AND r.kind='v2' AND r.status='running' AND r.graph_sealed
   AND NOT r.cancel_requested
   AND (w.v30_import_adoption_retry_after IS NULL
    OR w.v30_import_adoption_retry_after<=clock_timestamp())
   AND photo.v30_interchange_import_adoption_context(w.tenant_id,t.id,
    t.attempt,t.fencing_token,z.id) IS NOT NULL
   AND NOT EXISTS(SELECT 1 FROM photo.inbox receipt
    WHERE receipt.tenant_id=w.tenant_id AND receipt.consumer='v30-interchange-import'
     AND receipt.event_id=photo.v2_adoption_event_id(t.id,z.id))
  ORDER BY coalesce(w.v30_import_adoption_retry_after,'-infinity'::timestamptz),
   r.created_at,w.id LIMIT 1 FOR UPDATE OF w SKIP LOCKED
 ), claimed AS (
  UPDATE photo.v2_workflows w
  SET v30_import_adoption_retry_after=clock_timestamp()+interval '31 minutes',
   v30_import_adoption_token=gen_random_uuid()
  FROM candidate c WHERE w.tenant_id=c.tenant_id AND w.id=c.workflow_id
  RETURNING w.tenant_id,w.id,w.v30_import_adoption_token
 )
 SELECT c.*,claimed.v30_import_adoption_token FROM candidate c JOIN claimed
  ON claimed.tenant_id=c.tenant_id AND claimed.id=c.workflow_id
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_claim_next_import_adoption() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_claim_next_import_adoption() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_claim_next_import_adoption() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_adoption_claim_current(p_tenant uuid,p_workflow uuid,
 p_run uuid,p_task uuid,p_result uuid,p_attempt integer,p_fence bigint,p_token uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_token IS NOT NULL AND p_token<>'00000000-0000-0000-0000-000000000000'::uuid
 AND EXISTS(SELECT 1 FROM photo.v2_workflows w
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
   AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  JOIN photo.tenants tenant ON tenant.id=w.tenant_id AND tenant.status='active'
  WHERE w.tenant_id=p_tenant AND w.id=p_workflow AND w.run_id=p_run
   AND w.workflow_type='interchange' AND w.renderer_version='v2.2'
   AND w.v30_import_adoption_token=p_token
   AND w.v30_import_adoption_retry_after>clock_timestamp()
   AND r.kind='v2' AND r.status='running' AND r.graph_sealed
   AND NOT r.cancel_requested AND t.id=p_task
   AND t.id=photo.v30_uuid5(w.id,'v2.interchange.import:0')
   AND t.operation='v2.interchange.import' AND t.operation_version='1'
   AND t.state='succeeded' AND NOT t.cancel_requested
   AND z.id=p_result AND t.attempt=p_attempt AND t.fencing_token=p_fence
   AND NOT EXISTS(SELECT 1 FROM photo.inbox receipt
    WHERE receipt.tenant_id=p_tenant AND receipt.consumer='v30-interchange-import'
     AND receipt.event_id=photo.v2_adoption_event_id(p_task,p_result)))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_renew_import_adoption_claim(p_tenant uuid,p_workflow uuid,
 p_run uuid,p_task uuid,p_result uuid,p_attempt integer,p_fence bigint,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v30_import_adoption_claim_current(p_tenant,p_workflow,p_run,p_task,
  p_result,p_attempt,p_fence,p_token) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.v2_workflows SET v30_import_adoption_retry_after=clock_timestamp()+interval '31 minutes'
 WHERE tenant_id=p_tenant AND id=p_workflow AND run_id=p_run
  AND v30_import_adoption_token=p_token
  AND v30_import_adoption_retry_after>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_renew_import_adoption_claim(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_renew_import_adoption_claim(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_renew_import_adoption_claim(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
