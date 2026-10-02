-- A fitted CandidateProfile is a future output. Open the evaluation barrier
-- only in the transaction that adopts its canonical artifact and fence.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.open_v23_learning_evaluate_barrier(p_tenant uuid,
 p_task uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; t photo.tasks%ROWTYPE;
 w photo.v2_workflows%ROWTYPE; z photo.task_results%ROWTYPE;
 a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL OR t.operation<>'learning.fit' OR
  t.operation_version<>'1' OR t.node_key<>'learning.fit:0' THEN
  RETURN false;
 END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id
  FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant
  AND run_id=t.run_id;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR
  NOT r.graph_sealed OR r.cancel_requested OR w.id IS NULL OR
  w.workflow_type<>'learning' OR w.renderer_version<>'v2.2' OR
  t.state<>'succeeded' OR t.canonical_result_id IS NULL OR
  t.cancel_requested OR
  (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=p_tenant
   AND other.run_id=r.id)<>1 OR
  EXISTS(SELECT 1 FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant
   AND e.run_id=r.id AND e.state<>'closed') OR
  NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
   AND i.consumer='v2-adopt' AND
   i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN
  RETURN false;
 END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant
  AND id=t.canonical_result_id AND task_id=t.id AND attempt=t.attempt
  AND fencing_token=t.fencing_token;
 IF z.id IS NULL OR jsonb_typeof(z.outputs)<>'array' OR
  jsonb_array_length(z.outputs)<>1 OR
  coalesce(z.outputs->0->>'artifact_id','') !~
   '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
  coalesce(z.outputs->0->>'sha256','') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V23_FIT_RESULT_INVALID' USING ERRCODE='23514';
 END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=(z.outputs->0->>'artifact_id')::uuid;
 IF a.id IS NULL OR a.sha256<>z.outputs->0->>'sha256' OR
  a.output_role<>'document' OR a.media_type<>'application/json' OR
  a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR
  a.created_by_task_id<>t.id OR a.produced_attempt<>t.attempt OR
  a.produced_fence<>t.fencing_token OR
  (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN
  RAISE EXCEPTION 'V23_FIT_ARTIFACT_INVALID' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.graph_expansions(tenant_id,run_id,parent_task_id,
  kind,state,expected_count)
 VALUES(p_tenant,r.id,t.id,'v2.learning.evaluate/1','open',1);
 UPDATE photo.runs SET graph_sealed=false WHERE tenant_id=p_tenant
  AND id=r.id AND status='running' AND graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'V23_EVALUATE_BARRIER_RACE' USING ERRCODE='40001';
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.open_v23_learning_evaluate_barrier(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.open_v23_learning_evaluate_barrier(uuid,uuid)
 FROM PUBLIC;

ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 RENAME TO adopt_v2_plan_v22_core;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_v22_core(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,
 p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE adopted boolean;
BEGIN
 adopted:=photo.adopt_v2_plan_v22_core(p_tenant,p_task,p_attempt,p_fence,
  p_result,p_inventory,p_digest);
 IF adopted THEN
  PERFORM photo.open_v23_learning_evaluate_barrier(p_tenant,p_task);
 END IF;
 RETURN adopted;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 TO photo_scheduler;
