-- +goose Up
-- A declined restoration candidate must have no content consumer; the exact
-- read-only before/after preview child does not count as Apply consumption.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_review_completion_ready(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.v28_restoration_review_selection(p_tenant,p_task) selected
  JOIN photo.tasks origin ON origin.tenant_id=p_tenant AND origin.id=p_task
  WHERE selected.route='candidate' OR (selected.route IN('base','skip')
   AND NOT EXISTS(SELECT 1 FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant
    AND edge.prerequisite_id=p_task)
   AND NOT EXISTS(SELECT 1 FROM photo.artifacts output
    JOIN photo.tasks consumer ON consumer.tenant_id=output.tenant_id
     AND consumer.id<>p_task
    JOIN photo.runs child ON child.tenant_id=consumer.tenant_id
     AND child.id=consumer.run_id
    WHERE output.tenant_id=p_tenant AND output.created_by_task_id=p_task
     AND NOT (child.kind='preview' AND child.v28_restoration_preview_task_id=p_task
      AND child.config_snapshot->>'source_result_id'=origin.canonical_result_id::text)
     AND (consumer.inputs @> jsonb_build_array(jsonb_build_object(
      'artifact_id',output.id::text,'sha256',output.sha256))
      OR EXISTS(SELECT 1 FROM photo.v2_task_inputs input
       WHERE input.tenant_id=consumer.tenant_id AND input.task_id=consumer.id
        AND input.artifact_id=output.id AND input.sha256=output.sha256)))));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_review_completion_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_review_completion_ready(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_review_completion_ready(uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_selected_run_ready(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.runs r
  JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
  JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
   AND z.task_id=t.id
  JOIN photo.reviews v ON v.tenant_id=t.tenant_id AND v.task_id=t.id
   AND v.run_id=r.id AND v.kind=t.operation
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
   AND r.status='waiting_review' AND r.graph_sealed AND NOT r.cancel_requested
   AND t.operation IN('portrait.glare','image.reflection') AND t.operation_version='1'
   AND t.state='succeeded' AND NOT t.cancel_requested
   AND z.review_required AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
   AND v.state='decided' AND v.revision=2
   AND (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=p_tenant
    AND other.run_id=p_run)=1
   AND (SELECT count(*) FROM photo.reviews other WHERE other.tenant_id=p_tenant
    AND other.run_id=p_run)=1
   AND (SELECT count(*) FROM photo.v28_restoration_review_selection(p_tenant,t.id))=1
   AND photo.v28_restoration_review_completion_ready(p_tenant,t.id)
   AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions expansion
    WHERE expansion.tenant_id=p_tenant AND expansion.run_id=p_run
     AND expansion.state<>'closed'));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_selected_run_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_selected_run_ready(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_selected_run_ready(uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.finalize_v28_restoration_selected_run() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate photo.runs%ROWTYPE;current_run photo.runs%ROWTYPE;
 cursor_value bigint;event_id uuid;
BEGIN
 SELECT r.* INTO candidate FROM photo.runs r JOIN photo.reviews v
  ON v.tenant_id=r.tenant_id AND v.run_id=r.id
 WHERE r.kind='v2' AND r.status='waiting_review'
  AND v.kind IN('portrait.glare','image.reflection') AND v.state='decided'
  AND photo.v28_restoration_selected_run_ready(r.tenant_id,r.id)
 ORDER BY r.created_at,r.id LIMIT 1;
 IF candidate.id IS NULL THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tenants tenant WHERE tenant.id=candidate.tenant_id
  AND tenant.status='active' FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO current_run FROM photo.runs r WHERE r.tenant_id=candidate.tenant_id
  AND r.id=candidate.id FOR UPDATE SKIP LOCKED;
 IF current_run.id IS NULL THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tasks t WHERE t.tenant_id=current_run.tenant_id
  AND t.run_id=current_run.id ORDER BY t.id FOR UPDATE;
 PERFORM 1 FROM photo.reviews v WHERE v.tenant_id=current_run.tenant_id
  AND v.run_id=current_run.id ORDER BY v.id FOR SHARE;
 IF NOT photo.v28_restoration_selected_run_ready(current_run.tenant_id,current_run.id)
 THEN RETURN false;END IF;
 IF current_run.revision>=9007199254740991
 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor)
  VALUES(current_run.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=current_run.tenant_id AND last_cursor<9007199254740991
  RETURNING last_cursor INTO cursor_value;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='completed',revision=revision+1
  WHERE tenant_id=current_run.tenant_id AND id=current_run.id
   AND status='waiting_review' AND revision=current_run.revision;
 IF NOT FOUND THEN RAISE EXCEPTION 'RESTORATION_REVIEW_FINALIZATION_RACE' USING ERRCODE='23514';END IF;
 event_id:=photo.v30_uuid5(current_run.id,'restoration:review:completed');
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(current_run.tenant_id,cursor_value,current_run.id,current_run.created_by,
  'principal','v2.run.completed',jsonb_build_object('run_id',current_run.id,
   'status','completed','revision',current_run.revision+1));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(current_run.tenant_id,event_id,'photo.events.v1','v2.run.completed',
  jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.completed',
   'tenant_id',current_run.tenant_id,'run_id',current_run.id,'status','completed'));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_v28_restoration_selected_run() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v28_restoration_selected_run()
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.finalize_v28_restoration_selected_run() TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
