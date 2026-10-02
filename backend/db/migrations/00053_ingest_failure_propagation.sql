-- +goose Up
-- One transition per transaction, using the same task lock order as admission.
-- +goose StatementBegin
CREATE FUNCTION photo.skip_failed_ingest_inspection() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;
BEGIN
 SELECT child.id INTO candidate FROM photo.tasks child
 JOIN photo.runs run ON run.tenant_id=child.tenant_id AND run.id=child.run_id
 JOIN photo.ingest_progress progress ON progress.tenant_id=run.tenant_id AND progress.run_id=run.id AND progress.phase='inspection'
 JOIN photo.ingest_node_plans plan ON plan.tenant_id=child.tenant_id AND plan.run_id=child.run_id AND plan.id=child.id AND plan.operation='asset.inspect'
 JOIN photo.task_edges edge ON edge.tenant_id=child.tenant_id AND edge.run_id=child.run_id AND edge.dependent_id=child.id AND edge.condition='success'
 JOIN photo.tasks parent ON parent.tenant_id=edge.tenant_id AND parent.run_id=edge.run_id AND parent.id=edge.prerequisite_id
 WHERE run.kind='ingest' AND run.graph_sealed AND NOT run.cancel_requested AND run.status IN ('queued','running')
 AND child.state='blocked' AND child.operation='asset.inspect' AND NOT child.cancel_requested
 AND parent.operation='asset.verify' AND parent.state IN ('failed','cancelled')
 ORDER BY run.created_at,run.id,plan.ordinal LIMIT 1;
 IF candidate IS NULL THEN RETURN 0;END IF;
 t:=photo.lock_task(candidate);
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF t.state<>'blocked' OR t.cancel_requested OR r.cancel_requested OR NOT r.graph_sealed OR r.status NOT IN ('queued','running')
 OR NOT EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=t.tenant_id AND p.run_id=t.run_id AND p.phase='inspection') THEN RETURN 0;END IF;
 IF NOT EXISTS(SELECT FROM photo.task_edges e JOIN photo.tasks parent ON parent.tenant_id=e.tenant_id AND parent.run_id=e.run_id AND parent.id=e.prerequisite_id
  WHERE e.tenant_id=t.tenant_id AND e.run_id=t.run_id AND e.dependent_id=t.id AND e.condition='success'
  AND parent.operation='asset.verify' AND parent.state IN ('failed','cancelled')) THEN RETURN 0;END IF;
 UPDATE photo.tasks SET state='skipped',terminal_reason='upstream_failed' WHERE tenant_id=t.tenant_id AND id=t.id;
 RETURN 1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.skip_failed_ingest_inspection() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.skip_failed_ingest_inspection() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.skip_failed_ingest_inspection() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
