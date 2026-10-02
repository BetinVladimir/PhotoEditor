-- +goose Up
-- Adoption and reopening the graph commit atomically, so finalization cannot
-- observe an adopted preparation node without its assessment barrier.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_capture_prepared(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task photo.tasks%ROWTYPE; run photo.runs%ROWTYPE;
BEGIN
 IF NOT photo.adopt_v2_plan(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest) THEN RETURN false; END IF;
 SELECT * INTO task FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO run FROM photo.runs WHERE tenant_id=p_tenant AND id=task.run_id;
 IF task.operation<>'capture.prepare' OR task.operation_version<>'1'
  OR task.node_key<>'capture.prepare:0' OR task.state<>'succeeded'
  OR run.status<>'running' OR NOT run.graph_sealed OR run.cancel_requested
  OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant
   AND run_id=run.id AND workflow_type='capture' AND renderer_version='v2.2')
  OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=run.id)<>1
  OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=run.id)
  OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=run.id)
 THEN RAISE EXCEPTION 'CAPTURE_PREPARATION_GRAPH_INVALID' USING ERRCODE='23514'; END IF;
 INSERT INTO photo.graph_expansions(tenant_id,run_id,parent_task_id,kind,state,expected_count)
 VALUES(p_tenant,run.id,task.id,'v2.capture.assess/1','open',1);
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1
 WHERE tenant_id=p_tenant AND id=run.id AND status='running' AND graph_sealed
  AND NOT cancel_requested AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'CAPTURE_PREPARATION_BARRIER_RACE' USING ERRCODE='40001'; END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_capture_prepared(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_capture_prepared(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_capture_prepared(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
