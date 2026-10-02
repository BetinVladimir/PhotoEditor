-- +goose Up
-- Only exact terminal child plans can settle a parent; missing parts never win.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_task_result_ready(p_tenant uuid,p_task uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
  JOIN photo.artifacts a ON a.tenant_id=z.tenant_id AND a.id::text=z.outputs->0->>'artifact_id'
  WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.state='succeeded' AND t.operation='render.compose' AND t.operation_version='1'
   AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND jsonb_array_length(z.outputs)=1
   AND a.sha256=z.outputs->0->>'sha256' AND a.output_role='document' AND a.media_type='application/json'
   AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_task_result_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_task_result_ready(uuid,uuid) FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_v26_render_batch() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo SET jit=off AS $$
DECLARE candidate photo.runs%ROWTYPE; parent photo.runs%ROWTYPE; child photo.runs%ROWTYPE;
 part jsonb; planned jsonb; t photo.tasks%ROWTYPE; counts record; next_status text; cursor_value bigint; has_error boolean:=false;
BEGIN
 SELECT r.* INTO candidate FROM photo.runs r WHERE r.kind='bulk'
  AND r.status IN('draft','queued','running','waiting_review') AND NOT r.cancel_requested
  AND r.config_snapshot->'schema_version'='1'::jsonb AND r.config_snapshot ?& ARRAY['policy_sha','auth_session_id','parts']
  AND jsonb_typeof(r.config_snapshot->'parts')='array' AND jsonb_array_length(r.config_snapshot->'parts')>0
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(r.config_snapshot->'parts') p
   LEFT JOIN photo.runs c ON c.tenant_id=r.tenant_id AND c.id::text=p->>'run_id' AND c.kind='v2'
    AND c.created_by=r.created_by AND c.input_snapshot_id=r.input_snapshot_id AND c.config_hash=p->>'config_sha'
    AND c.config_snapshot->>'project_id'=r.config_snapshot->>'project_id'
    AND c.config_snapshot->'v26_render'->>'auth_session_id'=r.config_snapshot->>'auth_session_id'
   WHERE c.id IS NULL OR c.status NOT IN('completed','completed_with_errors','failed','cancelled')
    OR jsonb_typeof(c.config_snapshot->'v26_render'->'nodes') IS DISTINCT FROM 'array'
    OR EXISTS(SELECT 1 FROM photo.reviews WHERE tenant_id=c.tenant_id AND run_id=c.id AND state='pending')
    OR EXISTS(SELECT 1 FROM photo.tasks candidate_task WHERE candidate_task.tenant_id=c.tenant_id AND candidate_task.run_id=c.id
     AND (candidate_task.state NOT IN('succeeded','failed','cancelled','skipped') OR (candidate_task.state='succeeded' AND NOT photo.v26_render_task_result_ready(candidate_task.tenant_id,candidate_task.id)))))
 ORDER BY r.created_at,r.id LIMIT 1;
 IF candidate.id IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE SKIP LOCKED;
 IF parent.id IS NULL OR parent.cancel_requested OR parent.status NOT IN('draft','queued','running','waiting_review') THEN RETURN false;END IF;
 FOR part IN SELECT value FROM jsonb_array_elements(parent.config_snapshot->'parts') LOOP
  SELECT * INTO child FROM photo.runs WHERE tenant_id=parent.tenant_id AND id=(part->>'run_id')::uuid FOR UPDATE SKIP LOCKED;
  IF child.id IS NULL OR child.kind<>'v2' OR child.created_by<>parent.created_by OR child.input_snapshot_id<>parent.input_snapshot_id
   OR child.config_hash IS DISTINCT FROM part->>'config_sha'
   OR child.config_snapshot->>'project_id' IS DISTINCT FROM parent.config_snapshot->>'project_id'
   OR child.config_snapshot->'v26_render'->>'auth_session_id' IS DISTINCT FROM parent.config_snapshot->>'auth_session_id'
   OR child.status NOT IN('completed','completed_with_errors','failed','cancelled')
   OR jsonb_typeof(child.config_snapshot->'v26_render'->'nodes') IS DISTINCT FROM 'array'
   OR jsonb_array_length(child.config_snapshot->'v26_render'->'nodes')<>(part->>'frame_count')::integer
   OR EXISTS(SELECT 1 FROM photo.reviews WHERE tenant_id=child.tenant_id AND run_id=child.id AND state='pending') THEN RETURN false;END IF;
  has_error:=has_error OR child.status IN('completed_with_errors','failed','cancelled');
  FOR planned IN SELECT value FROM jsonb_array_elements(child.config_snapshot->'v26_render'->'nodes') LOOP
   SELECT * INTO t FROM photo.tasks WHERE tenant_id=child.tenant_id AND run_id=child.id AND id::text=planned->>'task_id';
   IF t.id IS NULL OR t.operation<>'render.compose' OR t.operation_version<>'1'
    OR t.params_artifact_id::text IS DISTINCT FROM planned->'params'->>'artifact_id'
    OR t.input_digest IS DISTINCT FROM planned->>'input_digest'
    OR t.state NOT IN('succeeded','failed','cancelled','skipped') THEN RETURN false;END IF;
   IF t.state='succeeded' AND NOT photo.v26_render_task_result_ready(t.tenant_id,t.id) THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 PERFORM set_config('app.tenant_id',parent.tenant_id::text,true);
 SELECT * INTO counts FROM photo.v26_render_batch_counts(parent.tenant_id,parent.id);
 IF counts.total<1 OR counts.total<>counts.succeeded+counts.failed+counts.skipped OR counts.review<>0 THEN RETURN false;END IF;
 next_status:=CASE WHEN has_error OR counts.failed>0 THEN CASE WHEN counts.succeeded>0 THEN 'completed_with_errors' ELSE 'failed' END ELSE 'completed' END;
 IF parent.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(parent.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=parent.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status=next_status,graph_sealed=true,revision=revision+1 WHERE id=parent.id AND tenant_id=parent.tenant_id;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(parent.tenant_id,cursor_value,parent.id,parent.created_by,'principal','run.'||next_status,
  jsonb_build_object('run_id',parent.id,'status',next_status,'revision',parent.revision+1));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_v26_render_batch() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v26_render_batch() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_v26_render_batch() TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v26_terminal_batch_child() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.kind='v2' AND OLD.status IN('completed','completed_with_errors','failed','cancelled')
  AND NEW.status NOT IN('completed','completed_with_errors','failed','cancelled') AND EXISTS(
  SELECT 1 FROM photo.runs parent CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(parent.config_snapshot->'parts')='array' THEN parent.config_snapshot->'parts' ELSE '[]'::jsonb END) part
  WHERE parent.tenant_id=OLD.tenant_id AND parent.kind='bulk' AND parent.status IN('completed','completed_with_errors','failed','cancelled')
   AND parent.config_snapshot->'schema_version'='1'::jsonb AND parent.config_snapshot ?& ARRAY['policy_sha','auth_session_id']
   AND part->>'run_id'=OLD.id::text AND part->>'config_sha'=OLD.config_hash
   AND parent.created_by=OLD.created_by AND parent.input_snapshot_id=OLD.input_snapshot_id
   AND parent.config_snapshot->>'project_id'=OLD.config_snapshot->>'project_id'
   AND parent.config_snapshot->>'auth_session_id'=OLD.config_snapshot->'v26_render'->>'auth_session_id') THEN
  RAISE EXCEPTION 'V26_BATCH_ALREADY_TERMINAL' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v26_terminal_batch_child() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v26_terminal_batch_child() FROM PUBLIC;
CREATE TRIGGER v26_terminal_batch_child BEFORE UPDATE OF status ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v26_terminal_batch_child();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
