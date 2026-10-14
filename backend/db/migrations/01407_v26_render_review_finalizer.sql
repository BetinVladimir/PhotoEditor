-- +goose Up
-- Preserve existing finalization barriers and quota/event behavior. Publish
-- render candidate reviews only after the prior finalizer accepts adoption.
-- +goose StatementBegin
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) RENAME TO finalize_v2_run_pre_render_reviews;
REVOKE ALL ON FUNCTION photo.finalize_v2_run_pre_render_reviews(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.finalize_v2_run(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE outcome text; t photo.tasks%ROWTYPE; root_id uuid; candidate_id uuid; base_id uuid; created uuid; matches integer; run_row photo.runs%ROWTYPE; cursor_value bigint; event_id uuid;
BEGIN
 outcome:=photo.finalize_v2_run_pre_render_reviews(p_tenant,p_run);
 IF outcome<>'waiting_review' THEN RETURN outcome;END IF;
 FOR t IN SELECT task.* FROM photo.tasks task JOIN photo.task_results z
 ON z.tenant_id=task.tenant_id AND z.task_id=task.id AND z.id=task.canonical_result_id
 WHERE task.tenant_id=p_tenant AND task.run_id=p_run AND task.operation='render.compose'
 AND task.operation_version='1' AND task.state='succeeded' AND z.review_required
 ORDER BY task.id LOOP
  SELECT count(*),min(a.id::text)::uuid INTO matches,root_id
  FROM photo.v2_workflows w JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id AND l.project_id=w.project_id
  JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
  WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND l.ordinal=0
  AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex');
  IF matches<>1 THEN RAISE EXCEPTION 'RENDER_REVIEW_ROOT_MISSING' USING ERRCODE='23514';END IF;
  SELECT count(*),min(l.artifact_id::text)::uuid INTO matches,candidate_id
  FROM photo.v2_workflows w JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id AND l.project_id=w.project_id
  WHERE w.tenant_id=p_tenant AND w.run_id=p_run
  AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex');
  IF matches<>1 THEN RAISE EXCEPTION 'RENDER_REVIEW_CANDIDATE_MISSING' USING ERRCODE='23514';END IF;
  SELECT count(*),min((((n->>'params_jcs')::jsonb)->'scalars'->'source'->'source'->>'artifact_id'))::uuid INTO matches,base_id
  FROM photo.runs r CROSS JOIN LATERAL jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes') n
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND n->>'task_id'=t.id::text;
  IF matches<>1 OR base_id IS NULL THEN RAISE EXCEPTION 'RENDER_REVIEW_SOURCE_MISSING' USING ERRCODE='23514';END IF;
  created:=photo.create_v26_render_review(p_tenant,t.id,root_id,candidate_id,base_id);
  IF created IS NULL THEN RAISE EXCEPTION 'RENDER_REVIEW_NOT_PUBLISHED' USING ERRCODE='23514';END IF;
 END LOOP;
 SELECT * INTO run_row FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 -- Only accepted render reviews complete here. Reject/skip require explicit
 -- result-selection handling and must never promote the rejected candidate.
 IF NOT EXISTS(SELECT 1 FROM photo.tasks task JOIN photo.task_results z ON z.tenant_id=task.tenant_id AND z.task_id=task.id AND z.id=task.canonical_result_id
 WHERE task.tenant_id=p_tenant AND task.run_id=p_run AND z.review_required) THEN RETURN outcome;END IF;
 IF run_row.status<>'waiting_review' OR run_row.cancel_requested OR NOT run_row.graph_sealed
 OR EXISTS(SELECT 1 FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND (v.kind<>'render.compose' OR v.state<>'decided' OR v.decision<>'accept' OR v.decision_route<>'candidate'))
 OR EXISTS(SELECT 1 FROM photo.tasks task JOIN photo.task_results z ON z.tenant_id=task.tenant_id AND z.task_id=task.id AND z.id=task.canonical_result_id
 WHERE task.tenant_id=p_tenant AND task.run_id=p_run AND z.review_required AND
 (task.operation<>'render.compose' OR task.operation_version<>'1' OR NOT EXISTS(
 SELECT 1 FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.task_id=task.id
 AND v.kind='render.compose' AND v.state='decided' AND v.decision='accept' AND v.decision_route='candidate'
 AND v.decided_by IS NOT NULL AND v.decided_at IS NOT NULL AND v.decision_hash IS NOT NULL))) THEN RETURN outcome;END IF;
 IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 event_id:=photo.v30_uuid5(p_run,'render:review:completed');
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,cursor_value,p_run,run_row.created_by,'principal','v2.run.completed',jsonb_build_object('run_id',p_run,'status','completed','revision',run_row.revision+1));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v2.run.completed',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.completed','tenant_id',p_tenant,'run_id',p_run,'status','completed'));
 RETURN 'completed';
END $$;
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v2_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_v2_run(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
