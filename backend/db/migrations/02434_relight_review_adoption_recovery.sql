-- +goose Up
-- An inbox marker predating review publication must not strand a running relight.
-- Recovery reuses physical adoption verification; it does not trust an inbox alone.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.create_v28_relight_review(uuid,uuid,uuid,text)'::regprocedure);
 needle:='AND a.sha256=encode(sha256(convert_to(p_body,''UTF8'')),''hex'')';
 replacement:=needle||' AND a.bytes=octet_length(p_body)'
  ||' AND a.object_version IS NOT NULL AND a.object_version NOT IN('''',''null'')';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'RELIGHT_PUBLICATION_PARAMS_PIN_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,replacement);
 definition:=pg_get_functiondef('photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text)'::regprocedure);
 needle:='IF NOT adopted THEN RETURN false;END IF;';
 replacement:=$patch$IF NOT adopted THEN
  IF NOT EXISTS(SELECT 1 FROM photo.tasks task JOIN photo.runs parent
    ON parent.tenant_id=task.tenant_id AND parent.id=task.run_id
   JOIN photo.inbox marker ON marker.tenant_id=task.tenant_id AND marker.consumer='v2-adopt'
    AND marker.event_id=photo.v2_adoption_event_id(task.id,p_result)
   JOIN photo.run_events event ON event.tenant_id=task.tenant_id AND event.run_id=task.run_id
    AND event.type='v2.task.adopted' AND event.payload->>'task_id'=task.id::text
    AND event.payload->>'result_id'=p_result::text
   WHERE task.tenant_id=p_tenant AND task.id=p_task AND task.operation='image.relight'
    AND task.operation_version='1' AND task.state='succeeded' AND NOT task.cancel_requested
    AND task.attempt=p_attempt AND task.fencing_token=p_fence AND task.canonical_result_id=p_result
    AND parent.kind='v2' AND parent.status='running' AND parent.graph_sealed AND NOT parent.cancel_requested
    AND event.payload->>'inventory_digest'=p_digest
    AND event.payload->>'sql_digest'=encode(sha256(convert_to(p_inventory::text,'UTF8')),'hex')
    AND NOT EXISTS(SELECT 1 FROM photo.reviews review WHERE review.tenant_id=p_tenant AND review.task_id=p_task))
   THEN RETURN false;END IF;
  -- The dedicated adopter can return false for a harmless exact core replay,
  -- or for an invalid proof. Distinguish those outcomes before publishing.
  IF p_evidence IS NULL OR photo.v28_relight_adoption_evidence(p_tenant,p_task,p_attempt,p_fence,p_result,
    p_body,p_depth,p_subject,p_guard,p_subject_root,p_guard_root) IS DISTINCT FROM p_evidence::jsonb
   THEN RETURN false;END IF;
 END IF;$patch$;
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'RELIGHT_REVIEW_RECOVERY_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_pending_adoptions() RENAME TO v2_pending_adoptions_pre_relight_review_recovery;
REVOKE ALL ON FUNCTION photo.v2_pending_adoptions_pre_relight_review_recovery() FROM PUBLIC,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.v2_pending_adoptions() RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH legacy AS (
 SELECT task.tenant_id,task.run_id,task.id AS task_id FROM photo.tasks task
 JOIN photo.runs parent ON parent.tenant_id=task.tenant_id AND parent.id=task.run_id
 JOIN photo.tenants tenant ON tenant.id=task.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=task.tenant_id AND workflow.run_id=task.run_id
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.id=task.canonical_result_id AND result.task_id=task.id
 JOIN photo.inbox marker ON marker.tenant_id=task.tenant_id AND marker.consumer='v2-adopt'
  AND marker.event_id=photo.v2_adoption_event_id(task.id,result.id)
 WHERE task.operation='image.relight' AND task.operation_version='1' AND task.state='succeeded'
  AND NOT task.cancel_requested AND parent.kind='v2' AND parent.status='running'
  AND parent.graph_sealed AND NOT parent.cancel_requested AND workflow.renderer_version='v2.2'
  AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token AND result.review_required
  AND NOT EXISTS(SELECT 1 FROM photo.reviews review WHERE review.tenant_id=task.tenant_id AND review.task_id=task.id)
 ORDER BY task.created_at,task.id LIMIT 16
 ), current_queue AS (
 SELECT prior.tenant_id,prior.run_id,prior.task_id FROM photo.v2_pending_adoptions_pre_relight_review_recovery() prior LIMIT (SELECT 64-count(*) FROM legacy)
 )
 SELECT legacy.tenant_id,legacy.run_id,legacy.task_id FROM legacy
 UNION ALL
 SELECT current_queue.tenant_id,current_queue.run_id,current_queue.task_id FROM current_queue;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_pending_adoptions() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_pending_adoptions() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v2_pending_adoptions() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
