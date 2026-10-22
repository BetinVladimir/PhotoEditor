-- +goose Up
-- Include retouch copies in shown child settlement and authenticated image projection.
-- Retain the installed scheduler entrypoint and transactional quota/events/settlement.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finalize_shown_semantic_apply_child(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant_row photo.tenants%ROWTYPE;r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 next_state text;next_cursor bigint;ev uuid;
BEGIN
 SELECT * INTO tenant_row FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF tenant_row.id IS NULL OR tenant_row.status<>'active' THEN RETURN 'stale';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'apply' OR NOT(r.config_snapshot ? 'shown_semantic') THEN RETURN 'stale';END IF;
 IF r.status IN ('completed','failed','cancelled') THEN PERFORM photo.settle_semantic_child(p_tenant,p_run);RETURN r.status;END IF;
 IF r.status NOT IN ('queued','running','cancelling') OR NOT r.graph_sealed THEN RETURN 'pending';END IF;
 IF jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes') IS DISTINCT FROM 'array' THEN RETURN 'stale';END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR UPDATE;
 IF jsonb_array_length(r.config_snapshot->'shown_semantic'->'nodes')<1
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>jsonb_array_length(r.config_snapshot->'shown_semantic'->'nodes')
 OR (SELECT count(DISTINCT node->>'task_id') FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') node)<>jsonb_array_length(r.config_snapshot->'shown_semantic'->'nodes')
 OR (SELECT count(DISTINCT node->>'target_id') FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') node)<>jsonb_array_length(r.config_snapshot->'shown_semantic'->'nodes')
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') node
 WHERE NOT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.semantic_targets target
 ON target.tenant_id=t.tenant_id AND target.child_run_id=t.run_id AND target.id::text=node->>'target_id'
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id::text=node->>'task_id'
 AND t.id=photo.semantic_uuid_v5(p_run,t.operation||':'||target.id::text)
 AND t.operation IN('semantic.adopt_shown_exposure','semantic.adopt_shown_removal','semantic.adopt_shown_background_cleanup','semantic.adopt_shown_retouch_region') AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.params_artifact_id::text=node->>'params_artifact_id' AND target.state='queued')) THEN RETURN 'stale';END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.state NOT IN ('succeeded','failed','cancelled','skipped')) THEN RETURN 'pending';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run) THEN RETURN 'pending';END IF;
 IF r.cancel_requested OR r.status='cancelling' THEN
  next_state:=CASE WHEN EXISTS(SELECT 1 FROM photo.run_events e WHERE e.tenant_id=p_tenant
   AND e.run_id=p_run AND e.type='v2.adoption.failed') THEN 'failed' ELSE 'cancelled' END;
 ELSIF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.state IN ('failed','cancelled','skipped')) THEN
  next_state:='failed';
 ELSE
  IF r.status<>'running' OR EXISTS(SELECT 1 FROM photo.tasks t
   WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND (t.state<>'succeeded'
    OR t.canonical_result_id IS NULL
    OR NOT EXISTS(SELECT 1 FROM photo.task_results result WHERE result.tenant_id=t.tenant_id
     AND result.id=t.canonical_result_id AND result.task_id=t.id
     AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token)
    OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
     AND i.consumer='v2-adopt'
     AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)))) THEN
   RETURN 'pending';
  END IF;
  next_state:='completed';
 END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=p_tenant
  AND x.holder_type='run' AND x.holder_id=p_run AND x.state='reserved' FOR UPDATE;
 IF q.id IS NOT NULL THEN
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes
   WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_RESERVATION_INCONSISTENT' USING ERRCODE='23514';END IF;
  UPDATE photo.quota_reservations SET bytes=0,
   state=CASE WHEN next_state IN ('failed','cancelled') THEN 'released' ELSE 'consumed' END
   WHERE tenant_id=p_tenant AND id=q.id;
 END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status=next_state,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 ev:=photo.semantic_uuid_v5(p_run,'semantic.child:'||next_state);
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
  VALUES(p_tenant,next_cursor,p_run,r.created_by,'principal','semantic.child.'||next_state,
   jsonb_build_object('run_id',p_run,'status',next_state));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
  VALUES(p_tenant,ev,'photo.events.v1','semantic.child.'||next_state,
   jsonb_build_object('schema_version',1,'event_id',ev,'type','semantic.child.'||next_state,
    'tenant_id',p_tenant,'run_id',p_run,'status',next_state));
 PERFORM photo.settle_semantic_child(p_tenant,p_run);
 RETURN next_state;
END $$;

CREATE OR REPLACE FUNCTION photo.finalize_next_shown_semantic_apply_child() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record; outcome text;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 WHERE r.kind='apply' AND r.config_snapshot ? 'shown_semantic' AND r.graph_sealed AND r.status IN('queued','running','cancelling')
 AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND
 (t.state NOT IN('succeeded','failed','cancelled','skipped') OR
 (NOT r.cancel_requested AND r.status<>'cancelling'
 AND NOT EXISTS(SELECT 1 FROM photo.tasks failed WHERE failed.tenant_id=r.tenant_id AND failed.run_id=r.id AND failed.state IN('failed','cancelled','skipped'))
 AND t.state='succeeded' AND
 (t.canonical_result_id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))))))
 ORDER BY r.tenant_id,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN 0;END IF;
 outcome:=photo.finalize_shown_semantic_apply_child(candidate.tenant_id,candidate.id);
 RETURN CASE WHEN outcome IN('completed','failed','cancelled') THEN 1 ELSE 0 END;
END $$;
ALTER FUNCTION photo.finalize_shown_semantic_apply_child(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_shown_semantic_apply_child(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.finalize_shown_semantic_apply_child(uuid,uuid) TO photo_scheduler;
ALTER FUNCTION photo.finalize_next_shown_semantic_apply_child() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_next_shown_semantic_apply_child() FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.finalize_next_shown_semantic_apply_child() TO photo_scheduler;
-- +goose StatementEnd

-- Authenticated read projection; no URLs, version mutations or new Apply.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_shown_applied_image(p_tenant uuid,p_target uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(child_run_id uuid,task_id uuid,result_id uuid,artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.id,t.id,z.id,a.id,a.sha256 FROM photo.semantic_targets target
 JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id
 JOIN photo.v2_workflows w ON w.tenant_id=intent.tenant_id AND w.id=intent.workflow_id AND w.project_id=target.project_id
 JOIN photo.v2_review_batches b ON b.tenant_id=target.tenant_id AND b.id=target.batch_id
 AND b.workflow_id=w.id AND b.project_id=w.project_id AND b.child_run_id=target.child_run_id AND b.action='apply'
 JOIN photo.runs r ON r.tenant_id=target.tenant_id AND r.id=target.child_run_id AND r.kind='apply'
 AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 AND t.id=photo.semantic_uuid_v5(r.id,t.operation||':'||target.id::text)
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid
 AND a.sha256=z.outputs->0->>'sha256' AND a.created_by_task_id=t.id
 AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token AND a.output_role='working_image'
 JOIN photo.tenants tenant ON tenant.id=target.tenant_id AND tenant.status='active'
 WHERE target.tenant_id=p_tenant AND target.id=p_target AND target.state='applied'
 AND t.operation IN('semantic.adopt_shown_exposure','semantic.adopt_shown_removal','semantic.adopt_shown_background_cleanup','semantic.adopt_shown_retouch_region') AND t.operation_version='1' AND t.state='succeeded'
 AND NOT t.cancel_requested AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND r.config_snapshot->>'project_id'=target.project_id::text
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n
 WHERE n->>'task_id'=t.id::text AND n->>'target_id'=target.id::text AND n->>'params_artifact_id'=t.params_artifact_id::text)
 AND photo.v2_editor_authorized(p_tenant,target.project_id,p_actor,p_session)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=a.tenant_id
 AND l.project_id=target.project_id AND l.workflow_id=w.id AND l.artifact_id=a.id AND l.sha256=a.sha256);
$$;
ALTER FUNCTION photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
