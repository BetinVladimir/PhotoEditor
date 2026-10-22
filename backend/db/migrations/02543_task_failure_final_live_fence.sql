-- +goose Up
-- Patch the only installed failure writer without replacing its identity/ACL.
-- +goose StatementBegin
DO $$
DECLARE target oid:=to_regprocedure('photo.commit_task_failure(uuid,integer,bigint,uuid,jsonb,jsonb)');
 source text; definition text;
 marker constant text := ' UPDATE photo.tasks SET state=CASE WHEN cancelled THEN ''cancelled'' ELSE ''retry_wait'' END,lease_until=NULL,next_attempt_at=NULL,';
 guard constant text := $guard$
 -- 02543: final failure authority precedes the last lease clock.
 PERFORM worker_id FROM photo.worker_registrations w
 WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker
 AND w.db_principal=session_user AND w.enabled FOR SHARE;
 IF NOT FOUND THEN RETURN 'stale'; END IF;
 IF NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,
  t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN
  RETURN 'stale';
 END IF;
 -- 02543: no authority/helper wait may follow this clock before mutation.
 IF t.state<>'running' OR t.attempt IS DISTINCT FROM p_attempt
 OR t.fencing_token IS DISTINCT FROM p_fence OR t.worker_id IS DISTINCT FROM p_worker
 OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR slot.id IS NULL OR slot.fencing_token IS DISTINCT FROM p_fence
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp() THEN
  RETURN 'stale';
 END IF;
$guard$;
BEGIN
 SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
 WHERE p.oid=target AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql');
 IF source IS NULL OR strpos(source,'-- 02543:')>0
 OR strpos(source,'t:=photo.lock_task(p_task);')=0
 OR strpos(source,'IF old_error=p_error AND old_metrics=p_metrics')=0
 OR strpos(source,'SELECT t.cancel_requested OR r.cancel_requested INTO cancelled')=0
 OR (length(source)-length(replace(source,marker,'')))/length(marker)<>1 THEN
  RAISE EXCEPTION 'TASK_FAILURE_BASE_SOURCE_SHAPE';
 END IF;
 EXECUTE replace(definition,source,replace(source,marker,guard||marker));
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
