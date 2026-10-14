-- +goose Up
-- Finish a waiting render run only from exact decided selections. The
-- immutable producer task/result and its candidate artifacts are never
-- rewritten. This pass handles reject/skip; the existing accept-only pass
-- remains responsible for all-candidate runs.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_selected_run_ready(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.runs r
  JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
   AND r.status='waiting_review' AND r.graph_sealed AND NOT r.cancel_requested
   AND EXISTS(SELECT 1 FROM photo.reviews v
    WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.kind='render.compose'
     AND v.state='decided' AND v.decision IN('reject','skip'))
   AND EXISTS(SELECT 1 FROM photo.tasks task
    WHERE task.tenant_id=p_tenant AND task.run_id=p_run)
   AND NOT EXISTS(SELECT 1 FROM photo.tasks task
    WHERE task.tenant_id=p_tenant AND task.run_id=p_run
     AND (task.operation<>'render.compose' OR task.operation_version<>'1'
      OR task.state<>'succeeded' OR task.cancel_requested
      OR task.canonical_result_id IS NULL
      OR NOT EXISTS(SELECT 1 FROM photo.inbox i
       WHERE i.tenant_id=task.tenant_id AND i.consumer='v2-adopt'
        AND i.event_id=photo.v2_adoption_event_id(task.id,task.canonical_result_id))
      OR NOT photo.v26_render_task_result_ready(task.tenant_id,task.id)))
   AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions e
    WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.state<>'closed')
   AND NOT EXISTS(SELECT 1 FROM photo.reviews v
    LEFT JOIN photo.tasks task ON task.tenant_id=v.tenant_id AND task.id=v.task_id
     AND task.run_id=v.run_id
    LEFT JOIN photo.task_results z ON z.tenant_id=task.tenant_id AND z.task_id=task.id
     AND z.id=task.canonical_result_id
    WHERE v.tenant_id=p_tenant AND v.run_id=p_run
     AND (v.kind<>'render.compose' OR v.state<>'decided' OR v.revision<>2
      OR task.id IS NULL OR task.operation<>'render.compose'
      OR task.operation_version<>'1' OR z.id IS NULL OR NOT z.review_required
      OR NOT photo.v26_render_review_completion_ready(v.tenant_id,v.task_id)
      OR (SELECT count(*) FROM photo.v26_render_review_selection(v.tenant_id,v.task_id))<>1))
   AND NOT EXISTS(SELECT 1 FROM photo.tasks task
    JOIN photo.task_results z ON z.tenant_id=task.tenant_id AND z.task_id=task.id
     AND z.id=task.canonical_result_id
    WHERE task.tenant_id=p_tenant AND task.run_id=p_run AND z.review_required
     AND (task.operation<>'render.compose' OR task.operation_version<>'1'
      OR NOT EXISTS(SELECT 1 FROM photo.reviews v
       WHERE v.tenant_id=task.tenant_id AND v.run_id=task.run_id
        AND v.task_id=task.id AND v.kind='render.compose' AND v.state='decided'
        AND photo.v26_render_review_completion_ready(v.tenant_id,v.task_id))))
 );
$$;
ALTER FUNCTION photo.v26_render_selected_run_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_selected_run_ready(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker;

CREATE FUNCTION photo.finalize_v26_render_selected_run() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate photo.runs%ROWTYPE; current_run photo.runs%ROWTYPE;
 cursor_value bigint; event_id uuid;
BEGIN
 SELECT r.* INTO candidate FROM photo.runs r
 WHERE r.kind='v2' AND r.status='waiting_review'
  AND photo.v26_render_selected_run_ready(r.tenant_id,r.id)
 ORDER BY r.created_at,r.id LIMIT 1;
 IF candidate.id IS NULL THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tenants tenant WHERE tenant.id=candidate.tenant_id
  AND tenant.status='active' FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO current_run FROM photo.runs r
  WHERE r.tenant_id=candidate.tenant_id AND r.id=candidate.id FOR UPDATE SKIP LOCKED;
 IF current_run.id IS NULL THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tasks task WHERE task.tenant_id=current_run.tenant_id
  AND task.run_id=current_run.id ORDER BY task.id FOR UPDATE;
 PERFORM 1 FROM photo.reviews v WHERE v.tenant_id=current_run.tenant_id
  AND v.run_id=current_run.id ORDER BY v.id FOR SHARE;
 IF NOT photo.v26_render_selected_run_ready(current_run.tenant_id,current_run.id)
  THEN RETURN false;END IF;
 IF current_run.revision>=9007199254740991 THEN
  RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor)
  VALUES(current_run.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=current_run.tenant_id AND last_cursor<9007199254740991
  RETURNING last_cursor INTO cursor_value;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='completed',revision=revision+1
  WHERE tenant_id=current_run.tenant_id AND id=current_run.id
   AND status='waiting_review' AND revision=current_run.revision;
 IF NOT FOUND THEN RAISE EXCEPTION 'RENDER_REVIEW_FINALIZATION_RACE' USING ERRCODE='23514';END IF;
 event_id:=photo.v30_uuid5(current_run.id,'render:review:completed');
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(current_run.tenant_id,cursor_value,current_run.id,current_run.created_by,
  'principal','v2.run.completed',jsonb_build_object('run_id',current_run.id,
  'status','completed','revision',current_run.revision+1));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(current_run.tenant_id,event_id,'photo.events.v1','v2.run.completed',
  jsonb_build_object('schema_version',1,'event_id',event_id,
   'type','v2.run.completed','tenant_id',current_run.tenant_id,
   'run_id',current_run.id,'status','completed'));
 RETURN true;
END $$;
ALTER FUNCTION photo.finalize_v26_render_selected_run() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v26_render_selected_run()
 FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.finalize_v26_render_selected_run() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
