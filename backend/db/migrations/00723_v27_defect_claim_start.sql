-- +goose Up
-- Successful native claim starts the sealed preparation run atomically.
-- Preserve the public function OID and the existing Apply start behavior.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record; task_row photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE;
BEGIN
 SELECT * INTO claimed FROM photo.claim_task_before_semantic_start(p_task,p_attempt,p_worker,p_command);
 IF claimed.outcome='claimed' THEN
  SELECT * INTO task_row FROM photo.tasks WHERE id=p_task;
  SELECT * INTO run_row FROM photo.runs WHERE tenant_id=task_row.tenant_id AND id=task_row.run_id FOR UPDATE;
  IF run_row.kind='v2' AND run_row.config_snapshot ? 'v27_defect_prepare' THEN
   IF task_row.operation IS DISTINCT FROM 'defects.propose' OR task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running')
    OR run_row.config_snapshot->'v27_defect_prepare'->>'task_id' IS DISTINCT FROM task_row.id::text
    OR run_row.config_snapshot->'v27_defect_prepare'->>'input_digest' IS DISTINCT FROM task_row.input_digest
    OR NOT photo.v2_input_closure_ready(task_row.id)
    OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=run_row.tenant_id AND run_id=run_row.id AND workflow_type='semantic' AND renderer_version='v2.2') THEN
    RAISE EXCEPTION 'SEMANTIC_DEFECT_START_STALE' USING ERRCODE='40001';END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
  IF run_row.kind='apply' AND run_row.config_snapshot ? 'approved_semantic' THEN
   IF NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running') THEN
    RAISE EXCEPTION 'SEMANTIC_APPLY_START_STALE' USING ERRCODE='40001';END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
 END IF;
 RETURN QUERY SELECT claimed.outcome,claimed.fence,claimed.lease_until,claimed.deadline,claimed.context;
END $$;
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
