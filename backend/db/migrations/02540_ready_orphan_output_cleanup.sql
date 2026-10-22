-- +goose Up
-- Depends on 02538 refund accounting and 02539 confirmed artifact storage settlement.
ALTER TABLE photo.output_uploads ADD COLUMN cleanup_ready_output boolean NOT NULL DEFAULT false;
-- +goose StatementBegin
ALTER FUNCTION photo.claim_output_cleanup() RENAME TO claim_output_cleanup_pending;
REVOKE ALL ON FUNCTION photo.claim_output_cleanup_pending() FROM PUBLIC,photo_maintenance;
ALTER FUNCTION photo.finish_output_cleanup(uuid,uuid) RENAME TO finish_output_cleanup_pending;
REVOKE ALL ON FUNCTION photo.finish_output_cleanup_pending(uuid,uuid) FROM PUBLIC,photo_maintenance;
CREATE FUNCTION photo.output_ready_orphan(p_upload uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 WHERE u.id=p_upload AND (u.state='ready' OR (u.state='cleaned' AND u.cleanup_ready_output))
 AND a.created_by_task_id=u.task_id AND a.produced_attempt=u.attempt AND a.produced_fence=u.fence
 AND a.bucket=u.bucket AND a.object_key=u.object_key AND a.object_version=u.object_version
 AND a.bytes=u.bytes AND a.sha256=u.sha256 AND a.media_type=u.media_type AND a.output_role=u.role
 AND a.state IN('ready','deleting','deleted') AND (a.state<>'deleted' OR a.gc_storage_settled)
 AND NOT(t.state='running' AND t.attempt=u.attempt AND t.fencing_token=u.fence
  AND t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=u.tenant_id AND z.task_id=u.task_id
  AND z.attempt=u.attempt AND z.fencing_token=u.fence)
 AND NOT photo.artifact_has_references(u.tenant_id,u.artifact_id))
$$;
ALTER FUNCTION photo.output_ready_orphan(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.output_ready_orphan(uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
CREATE FUNCTION photo.claim_output_cleanup()
RETURNS TABLE(upload_id uuid,cleanup_token uuid,tenant_id uuid,task_id uuid,artifact_id uuid,attempt integer,fence bigint,bucket text,object_key text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid;t photo.tasks%ROWTYPE;u photo.output_uploads%ROWTYPE;a photo.artifacts%ROWTYPE;token uuid:=gen_random_uuid();
BEGIN
 SELECT x.id INTO candidate FROM photo.output_uploads x
 WHERE (x.state='ready' OR (x.state='cleaned' AND x.cleanup_ready_output AND x.cleanup_after<=clock_timestamp()))
 AND (x.cleanup_lease_until IS NULL OR x.cleanup_lease_until<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts active_gc WHERE active_gc.tenant_id=x.tenant_id AND active_gc.id=x.artifact_id AND active_gc.state='deleting' AND active_gc.gc_lease_until>clock_timestamp())
 AND photo.output_ready_orphan(x.id) ORDER BY x.cleanup_after,x.id LIMIT 1;
 IF candidate IS NULL THEN RETURN QUERY SELECT * FROM photo.claim_output_cleanup_pending();RETURN;END IF;
 SELECT * INTO u FROM photo.output_uploads x WHERE x.id=candidate;
 t:=photo.lock_task(u.task_id);
 SELECT * INTO u FROM photo.output_uploads x WHERE x.id=candidate AND x.tenant_id=t.tenant_id FOR UPDATE;
 SELECT * INTO a FROM photo.artifacts x WHERE x.id=u.artifact_id AND x.tenant_id=u.tenant_id FOR UPDATE;
 IF u.id IS NULL OR (u.state='cleaned' AND u.cleanup_after>clock_timestamp()) OR (u.cleanup_lease_until IS NOT NULL AND u.cleanup_lease_until>clock_timestamp())
 OR (a.state='deleting' AND a.gc_lease_until>clock_timestamp()) OR NOT photo.output_ready_orphan(u.id) THEN RETURN;END IF;
 -- Keep ready receipt metadata intact until the shared GC function settles its charge.
 UPDATE photo.output_uploads SET cleanup_ready_output=true,cleanup_token=token,
  cleanup_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=u.id;
 IF a.state<>'deleted' THEN
  UPDATE photo.artifacts SET state='deleting',gc_token=token,gc_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=a.id;
 END IF;
 RETURN QUERY SELECT u.id,token,u.tenant_id,u.task_id,u.artifact_id,u.attempt,u.fence,u.bucket,u.object_key;
END $$;
CREATE FUNCTION photo.finish_output_cleanup(p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.output_uploads%ROWTYPE;u photo.output_uploads%ROWTYPE;t photo.tasks%ROWTYPE;
 a photo.artifacts%ROWTYPE;r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;refund boolean:=false;
BEGIN
 SELECT * INTO hint FROM photo.output_uploads WHERE id=p_upload;
 IF NOT FOUND THEN RETURN false;END IF;
 IF NOT hint.cleanup_ready_output THEN RETURN photo.finish_output_cleanup_pending(p_upload,p_token);END IF;
 t:=photo.lock_task(hint.task_id);
 SELECT * INTO u FROM photo.output_uploads WHERE id=p_upload AND tenant_id=t.tenant_id FOR UPDATE;
 IF u.id IS NULL OR p_token IS NULL OR u.cleanup_token IS DISTINCT FROM p_token
 OR u.cleanup_lease_until IS NULL OR u.cleanup_lease_until<=clock_timestamp()
 OR NOT photo.output_ready_orphan(u.id) THEN RETURN false;END IF;
 -- Canonical usage/run/task -> output -> quota -> artifact. Shared GC also takes usage first.
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF u.run_reservation_id IS NOT NULL THEN
  SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=u.tenant_id AND id=u.run_reservation_id FOR UPDATE;
 END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=u.tenant_id AND id=u.artifact_id FOR UPDATE;
 IF u.cleanup_lease_until<=clock_timestamp() OR NOT photo.output_ready_orphan(u.id) THEN RETURN false;END IF;
 IF a.state<>'deleted' THEN
  IF a.gc_token IS DISTINCT FROM p_token OR NOT photo.finish_artifact_gc(a.id,p_token) THEN RETURN false;END IF;
  SELECT * INTO a FROM photo.artifacts WHERE id=a.id;
 END IF;
 IF a.state<>'deleted' OR NOT a.gc_storage_settled THEN RETURN false;END IF;
 IF u.state='ready' THEN
  refund:=q.id IS NOT NULL AND q.holder_type='run' AND q.holder_id=t.run_id
   AND q.state='reserved' AND q.expires_at>clock_timestamp() AND q.principal_type='user'
   AND q.principal_id=r.created_by AND q.share_id IS NULL AND q.files=0
   AND r.status IN('queued','running','waiting_review') AND NOT r.cancel_requested AND NOT t.cancel_requested
   AND EXISTS(SELECT 1 FROM photo.tenants WHERE id=u.tenant_id AND status='active')
   AND q.bytes::numeric+u.bytes<=9007199254740991;
  IF refund AND NOT EXISTS(SELECT 1 FROM photo.tenant_usage usage JOIN photo.tenants tenant ON tenant.id=usage.tenant_id
   WHERE usage.tenant_id=u.tenant_id AND usage.storage_bytes::numeric+usage.reserved_bytes+u.bytes
    <=(tenant.quotas->>'storage_bytes')::numeric
   AND usage.storage_bytes::numeric+usage.reserved_bytes+u.bytes<=9007199254740991) THEN
   -- A separate GC may have settled earlier; never reclaim capacity another run now owns.
   RETURN false;
  END IF;
  IF refund THEN
   UPDATE photo.quota_reservations SET bytes=bytes+u.bytes WHERE tenant_id=q.tenant_id AND id=q.id;
   UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+u.bytes WHERE tenant_id=u.tenant_id
    AND storage_bytes::numeric+reserved_bytes+u.bytes<=9007199254740991;
   IF NOT FOUND THEN RAISE EXCEPTION 'OUTPUT_REFUND_ACCOUNTING_INCONSISTENT' USING ERRCODE='23514';END IF;
  END IF;
 END IF;
 UPDATE photo.output_uploads SET state='cleaned',returned_to_run_reservation=returned_to_run_reservation OR refund,
  cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_after=clock_timestamp()+interval '24 hours' WHERE id=u.id;
 RETURN true;
END $$;
CREATE FUNCTION photo.task_retry_outputs_pending(p_task uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.output_uploads u
 JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.quota_reservations q ON q.tenant_id=u.tenant_id AND q.id=u.run_reservation_id
 WHERE t.id=p_task AND NOT u.returned_to_run_reservation AND q.holder_type='run' AND q.holder_id=r.id
 AND q.state='reserved' AND q.expires_at>clock_timestamp() AND q.principal_type='user'
 AND q.principal_id=r.created_by AND q.share_id IS NULL AND q.files=0
 AND r.status IN('queued','running','waiting_review') AND NOT r.cancel_requested AND NOT t.cancel_requested)
$$;
ALTER FUNCTION photo.task_retry_outputs_pending(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.task_retry_outputs_pending(uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
DO $$
DECLARE source text;definition text;updated text;
 discovery text:='WHERE state=''retry_wait'' AND next_attempt_at<=clock_timestamp() ORDER BY';
 locked text:='IF t.state<>''retry_wait'' OR t.next_attempt_at IS NULL OR t.next_attempt_at>clock_timestamp() THEN CONTINUE; END IF;';
BEGIN
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid='photo.activate_task_retries(integer)'::regprocedure AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR strpos(source,discovery)=0 OR strpos(source,locked)=0
 OR strpos(source,'task_retry_outputs_pending')>0 THEN RAISE EXCEPTION 'OUTPUT_RETRY_SETTLEMENT_SOURCE_SHAPE';END IF;
 updated:=replace(source,discovery,'WHERE state=''retry_wait'' AND next_attempt_at<=clock_timestamp() AND NOT photo.task_retry_outputs_pending(id) ORDER BY');
 updated:=replace(updated,locked,locked||E'\n  -- 02540: pending physical cleanup keeps retry_wait without consuming another attempt.\n  IF photo.task_retry_outputs_pending(t.id) THEN CONTINUE;END IF;');
 EXECUTE replace(definition,source,updated);
END $$;
ALTER FUNCTION photo.claim_output_cleanup() OWNER TO photo_fn_owner;
ALTER FUNCTION photo.finish_output_cleanup(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_output_cleanup(),photo.finish_output_cleanup(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.claim_output_cleanup(),photo.finish_output_cleanup(uuid,uuid) TO photo_maintenance;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
