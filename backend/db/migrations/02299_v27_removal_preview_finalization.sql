-- +goose Up
-- Execution finalization only: no photographic acceptance, Apply or early quota release.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_next_semantic_removal_preview() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;e jsonb;parent uuid;done text;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x
 JOIN photo.tenants tenant ON tenant.id=x.tenant_id AND tenant.status='active'
 JOIN photo.tasks task ON task.tenant_id=x.tenant_id AND task.run_id=x.id
 WHERE x.kind='preview' AND x.status IN('queued','running') AND x.graph_sealed AND NOT x.cancel_requested
 AND x.v27_preapply_preview_profile IS NOT NULL
 AND jsonb_typeof(x.config_snapshot->'semantic_removal_preview')='object'
 AND task.operation='semantic.preview_remove_object' AND task.operation_version='1'
 AND task.state IN('succeeded','failed','cancelled','skipped')
 AND task.resource_class='gpu'
 AND task.id=photo.semantic_uuid_v5(x.id,'semantic.preview_remove_object:'||
  (x.config_snapshot->'semantic_removal_preview'->'params'->>'target_id')::uuid::text)
 AND NOT EXISTS(SELECT 1 FROM photo.tasks extra WHERE extra.tenant_id=x.tenant_id AND extra.run_id=x.id AND extra.id<>task.id)
 -- Filter ineligible successes before LIMIT: one malformed/expired result must
 -- not starve every later terminal run. Authority is repeated under locks below.
 AND (task.state<>'succeeded' OR (NOT task.cancel_requested
  AND photo.semantic_removal_preview_run_current(x.tenant_id,x.id) IS TRUE
  AND photo.semantic_removal_preview_closure_ready(task.id) IS TRUE
  AND EXISTS(SELECT 1 FROM photo.task_results z JOIN photo.artifacts a
   ON a.tenant_id=z.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid AND a.sha256=z.outputs->0->>'sha256'
   WHERE z.tenant_id=task.tenant_id AND z.task_id=task.id AND z.id=task.canonical_result_id
   AND z.attempt=task.attempt AND z.fencing_token=task.fencing_token
   AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
   AND a.created_by_task_id=task.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
   AND a.output_role='preview_presentation' AND a.media_type='application/json'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 1048576
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))))
 ORDER BY x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id;
 e:=r.config_snapshot->'semantic_removal_preview'->'params';
 SELECT w.run_id INTO parent FROM photo.v2_workflows w WHERE w.tenant_id=r.tenant_id AND w.id=(e->>'workflow_id')::uuid;
 PERFORM id FROM photo.runs WHERE tenant_id=r.tenant_id AND id=parent FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=r.tenant_id AND id=(e->>'target_id')::uuid FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'preview' OR r.cancel_requested OR NOT r.graph_sealed
 OR r.status NOT IN('queued','running') OR r.revision>=9007199254740991
 OR r.v27_preapply_preview_profile IS NULL THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id
 AND id=photo.semantic_uuid_v5(r.id,'semantic.preview_remove_object:'||(e->>'target_id')::uuid::text) FOR UPDATE;
 IF NOT FOUND OR t.operation<>'semantic.preview_remove_object' OR t.operation_version<>'1'
 OR t.resource_class<>'gpu' OR t.state NOT IN('succeeded','failed','cancelled','skipped')
 OR EXISTS(SELECT 1 FROM photo.tasks extra WHERE extra.tenant_id=r.tenant_id AND extra.run_id=r.id AND extra.id<>t.id)
 THEN RETURN false;END IF;
 done:='failed';
 IF t.state='succeeded' THEN
  IF t.cancel_requested OR photo.semantic_removal_preview_run_current(r.tenant_id,r.id) IS DISTINCT FROM true
  OR photo.semantic_removal_preview_closure_ready(t.id) IS DISTINCT FROM true
  OR NOT EXISTS(SELECT 1 FROM photo.task_results z JOIN photo.artifacts a
   ON a.tenant_id=z.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid AND a.sha256=z.outputs->0->>'sha256'
   WHERE z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
   AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
   AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
   AND a.output_role='preview_presentation' AND a.media_type='application/json'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 1048576
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
  done:='completed';
 END IF;
 UPDATE photo.runs SET status=done,revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.finalize_next_semantic_removal_preview() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_next_semantic_removal_preview() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.finalize_next_semantic_removal_preview() TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER FUNCTION photo.finalize_next_semantic_preapply_preview() RENAME TO finalize_next_semantic_preapply_preview_before_removal;
REVOKE ALL ON FUNCTION photo.finalize_next_semantic_preapply_preview_before_removal()
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
CREATE FUNCTION photo.finalize_next_semantic_preapply_preview() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.finalize_next_semantic_preapply_preview_before_removal() THEN RETURN true;END IF;
 RETURN photo.finalize_next_semantic_removal_preview();
END $$;
ALTER FUNCTION photo.finalize_next_semantic_preapply_preview() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_next_semantic_preapply_preview()
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.finalize_next_semantic_preapply_preview() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
