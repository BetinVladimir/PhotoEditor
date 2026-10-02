-- +goose Up
-- Expansion can observe only the canonical result after its full closure has
-- been adopted. A succeeded worker task alone is insufficient evidence.
-- +goose StatementBegin
CREATE FUNCTION photo.capture_prepared_source(p_tenant uuid,p_run uuid)
RETURNS TABLE(task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT task.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.runs run JOIN photo.tenants tenant ON tenant.id=run.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=run.tenant_id AND w.run_id=run.id
 JOIN photo.tasks task ON task.tenant_id=run.tenant_id AND task.run_id=run.id
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.id=task.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=task.tenant_id AND a.id=(result.outputs->0->>'artifact_id')::uuid
  AND a.sha256=result.outputs->0->>'sha256' AND a.created_by_task_id=task.id
  AND a.produced_attempt=task.attempt AND a.produced_fence=task.fencing_token
 WHERE run.tenant_id=p_tenant AND run.id=p_run AND run.kind='v2' AND run.status='running'
  AND NOT run.cancel_requested AND w.workflow_type='capture' AND w.renderer_version='v2.2'
  AND task.operation='capture.prepare' AND task.operation_version='1' AND task.state='succeeded'
  AND NOT task.cancel_requested AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document'
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=task.tenant_id AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(task.id,result.id))
  AND EXISTS(SELECT 1 FROM photo.v2_artifact_links link WHERE link.tenant_id=task.tenant_id
   AND link.workflow_id=w.id AND link.artifact_id=a.id AND link.sha256=a.sha256);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_prepared_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_prepared_source(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_prepared_source(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
