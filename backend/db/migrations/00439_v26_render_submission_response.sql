-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.read_v26_render_submission_ticket(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid,p_key text,p_sha text)
RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT i.response_body FROM photo.idempotency_records i
 JOIN photo.v26_render_submission_state(p_tenant,p_batch,p_actor,p_session) scoped ON scoped.request_sha=p_sha
 WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=p_actor
  AND i.route='V2.Admit' AND i.key=p_key AND i.request_hash=p_sha AND i.resource_id=p_batch
  AND i.response_status=202 AND i.response_body IS NOT NULL
$$;
ALTER FUNCTION photo.read_v26_render_submission_ticket(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v26_render_submission_ticket(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v26_render_submission_ticket(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;

CREATE FUNCTION photo.complete_v26_render_submission(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid,p_key text,p_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent photo.runs%ROWTYPE; idem photo.idempotency_records%ROWTYPE; part jsonb;
 child_id uuid; desired text; cursor_value bigint; body jsonb; children jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v26_render_submission_state(p_tenant,p_batch,p_actor,p_session) scoped WHERE scoped.request_sha=p_sha) THEN RETURN NULL; END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch FOR UPDATE;
 SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
  AND principal_id=p_actor AND route='V2.Admit' AND key=p_key FOR UPDATE;
 IF idem.id IS NULL OR idem.request_hash<>p_sha OR idem.resource_id IS DISTINCT FROM p_batch THEN RETURN NULL; END IF;
 IF idem.response_status=202 THEN RETURN idem.response_body; END IF;
 IF idem.response_status IS NOT NULL OR idem.expires_at<=clock_timestamp()
  OR parent.cancel_requested OR parent.status NOT IN('draft','queued','running','waiting_review') THEN RETURN NULL; END IF;
 FOR part IN SELECT value FROM jsonb_array_elements(parent.config_snapshot->'parts') LOOP
  SELECT child.id INTO child_id FROM photo.runs child JOIN photo.v2_workflows workflow
   ON workflow.tenant_id=child.tenant_id AND workflow.run_id=child.id
  WHERE child.tenant_id=p_tenant AND child.id=(part->>'run_id')::uuid AND child.kind='v2'
   AND child.graph_sealed AND NOT child.cancel_requested AND child.status IN('queued','running','waiting_review')
   AND child.config_hash=part->>'config_sha' AND child.created_by=p_actor
   AND child.input_snapshot_id=parent.input_snapshot_id
   AND workflow.project_id=(parent.config_snapshot->>'project_id')::uuid
   AND child.config_snapshot->'v26_render'->>'auth_session_id'=p_session::text
  FOR SHARE OF child;
  IF child_id IS NULL THEN RETURN NULL; END IF;
 END LOOP;
 desired:=photo.v26_render_batch_desired_state(p_tenant,p_batch);
 IF desired IS NULL OR desired NOT IN('queued','running','waiting_review') THEN RETURN NULL; END IF;
 IF parent.status<>desired THEN
  IF parent.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514'; END IF;
  INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
  UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
  IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514'; END IF;
  UPDATE photo.runs SET status=desired,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_batch;
  INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
   VALUES(p_tenant,cursor_value,p_batch,p_actor,'principal','run.'||desired,
    jsonb_build_object('run_id',p_batch,'status',desired,'revision',parent.revision+1));
 END IF;
 SELECT jsonb_agg(value->'run_id' ORDER BY ordinal) INTO children
  FROM jsonb_array_elements(parent.config_snapshot->'parts') WITH ORDINALITY parts(value,ordinal);
 body:=jsonb_build_object('run_id',p_batch,'state',desired,'child_run_ids',children);
 UPDATE photo.idempotency_records SET response_status=202,response_body=body WHERE tenant_id=p_tenant AND id=idem.id;
 RETURN body;
END $$;
ALTER FUNCTION photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
