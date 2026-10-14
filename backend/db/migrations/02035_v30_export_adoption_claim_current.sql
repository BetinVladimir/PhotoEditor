-- +goose Up
-- A read-only fence for the scheduler's discovery lease. A true result is not
-- object-read permission or adoption authority; those need separate current
-- source, output and closure checks in their own transactions.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_claim_current(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(EXISTS(
  SELECT 1 FROM photo.v2_workflows w
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  JOIN photo.tenants tenant ON tenant.id=w.tenant_id AND tenant.status='active'
  JOIN photo.projects project ON project.tenant_id=w.tenant_id
   AND project.id=w.project_id AND project.archived_at IS NULL
   AND project.download_allowed
  JOIN photo.tasks task ON task.tenant_id=w.tenant_id AND task.run_id=w.run_id
   AND task.id=p_task AND task.operation='v2.interchange.export'
   AND task.operation_version='1' AND task.state='succeeded'
   AND NOT task.cancel_requested
  JOIN photo.task_results result ON result.tenant_id=task.tenant_id
   AND result.id=task.canonical_result_id AND result.task_id=task.id
   AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token
  JOIN photo.idempotency_records pending ON pending.tenant_id=w.tenant_id
   AND pending.principal_type='user' AND pending.principal_id=p_actor
   AND pending.route='Interchange.Export.Pending' AND pending.key=p_key
   AND pending.request_hash=p_request_sha AND pending.response_status IS NULL
   AND pending.expires_at>clock_timestamp()
  WHERE p_tenant IS NOT NULL AND p_workflow IS NOT NULL AND p_run IS NOT NULL
   AND p_task IS NOT NULL AND p_result IS NOT NULL AND p_actor IS NOT NULL
   AND p_token IS NOT NULL AND p_key IS NOT NULL AND length(p_key) BETWEEN 1 AND 120
   AND p_request_sha ~ '^[0-9a-f]{64}$'
   AND w.tenant_id=p_tenant AND w.id=p_workflow AND w.run_id=p_run
   AND w.workflow_type='interchange' AND w.renderer_version='v2.2'
   AND w.policy_sha=p_request_sha
   AND w.v30_export_adoption_token=p_token
   AND w.v30_export_adoption_retry_after>clock_timestamp()
   AND r.kind='v2' AND r.status='running' AND r.graph_sealed
   AND NOT r.cancel_requested AND r.created_by=p_actor
   AND r.config_snapshot->>'request_sha'=p_request_sha
   AND p_run=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key)
   AND p_workflow=photo.v30_uuid5(p_run,'workflow')
   AND p_task=photo.v30_uuid5(p_workflow,'v2.interchange.export:0')
   AND result.id=p_result
   AND pending.response_body->'frozen'->'request'->>'direction'='export'
   AND pending.response_body->'frozen'->'request'->>'project_id'=w.project_id::text
   AND pending.response_body->'frozen'->'request'->>'snapshot_id'=w.input_snapshot_id::text
   AND pending.response_body->'frozen'->'request'->>'format'
    IN ('photoeditor_bundle','xmp','captureone_sidecar')
   AND NOT EXISTS(SELECT 1 FROM photo.inbox receipt
    WHERE receipt.tenant_id=p_tenant AND receipt.consumer='v30-interchange-export'
     AND receipt.event_id=photo.v2_adoption_event_id(p_task,p_result))
 ),false)
$$;
ALTER FUNCTION photo.v30_export_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
