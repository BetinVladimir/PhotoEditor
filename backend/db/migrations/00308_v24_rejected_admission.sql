-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reject_v2_adoption(p_tenant uuid,p_task uuid,p_result uuid,p_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant_row photo.tenants%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 next_cursor bigint;ev uuid:=gen_random_uuid();
BEGIN
 IF p_code IS NULL OR p_code !~ '^[A-Z][A-Z0-9_]{0,79}$' THEN
  RAISE EXCEPTION 'V2_ADOPTION_FAILURE_CODE_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO tenant_row FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF tenant_row.id IS NULL OR tenant_row.status<>'active' THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested
  OR t.state<>'succeeded' OR t.canonical_result_id IS DISTINCT FROM p_result
  OR EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(p_task,p_result)) THEN RETURN false;END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1
  WHERE tenant_id=p_tenant AND id=r.id;
 -- A permanent capture result rejection also terminates durable admission.
 -- The preparation pointer retains the immutable workflow association.
 IF t.operation IN('capture.prepare','capture.assess') THEN
  UPDATE photo.capture_assessment_jobs j SET state='failed',error_code=p_code,
   workflow_id=NULL,lease_token=NULL,lease_until=NULL
  FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=r.id
   AND w.workflow_type='capture' AND j.tenant_id=p_tenant
   AND j.project_id=w.project_id AND j.preparation_workflow_id=w.id
   AND j.state IN('pending','leased','queued');
 END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
  VALUES(p_tenant,next_cursor,r.id,r.created_by,'principal','v2.adoption.failed',
   jsonb_build_object('task_id',p_task,'result_id',p_result,'code',p_code));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
  VALUES(p_tenant,ev,'photo.events.v1','v2.adoption.failed',
   jsonb_build_object('schema_version',1,'event_id',ev,'type','v2.adoption.failed',
    'tenant_id',p_tenant,'run_id',r.id,'task_id',p_task,'code',p_code));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reject_v2_adoption(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reject_v2_adoption(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reject_v2_adoption(uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
