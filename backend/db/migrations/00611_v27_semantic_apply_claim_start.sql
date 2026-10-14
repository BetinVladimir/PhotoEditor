-- +goose Up
-- A successful worker claim starts its sealed approved Apply child atomically.
-- +goose StatementBegin
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) RENAME TO claim_task_before_semantic_start;
REVOKE ALL ON FUNCTION photo.claim_task_before_semantic_start(uuid,integer,uuid,jsonb) FROM PUBLIC,photo_worker;
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record; task_row photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE;
BEGIN
 SELECT * INTO claimed FROM photo.claim_task_before_semantic_start(p_task,p_attempt,p_worker,p_command);
 IF claimed.outcome='claimed' THEN
  SELECT * INTO task_row FROM photo.tasks WHERE id=p_task;
  SELECT * INTO run_row FROM photo.runs WHERE tenant_id=task_row.tenant_id AND id=task_row.run_id FOR UPDATE;
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
