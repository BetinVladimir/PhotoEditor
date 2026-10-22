-- +goose Up
-- Preserve the installed wrapper chain and its ACL. Recheck the live attempt
-- after output FOR UPDATE waits, immediately before creating a canonical result.
-- +goose StatementBegin
DO $$
DECLARE target oid; source text; definition text; patched text; matches integer;
 marker constant text := ' INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest)';
 guard constant text := $guard$
 -- 02535: output locks must not carry an expired attempt into a result.
 IF t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR slot.id IS NULL OR slot.fencing_token IS DISTINCT FROM p_fence
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR NOT EXISTS(SELECT 1 FROM photo.worker_registrations w
  WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker
  AND w.db_principal=session_user AND w.enabled)
 OR NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,
  t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text; RETURN;
 END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(m->'outputs') output
  JOIN photo.artifacts artifact ON artifact.tenant_id=t.tenant_id
   AND artifact.id=(output->>'artifact_id')::uuid
  WHERE artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp()) THEN
  RAISE EXCEPTION 'OUTPUT_NOT_VERIFIED' USING ERRCODE='22023';
 END IF;
 IF t.cancel_requested OR EXISTS(SELECT 1 FROM photo.runs r
  WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.cancel_requested) THEN
  RETURN QUERY SELECT 'cancelled',NULL::uuid,NULL::text; RETURN;
 END IF;
$guard$;
BEGIN
 SELECT count(*),min(p.oid::bigint)::oid INTO matches,target
 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND strpos(p.prosrc,marker)>0;
 IF matches<>1 THEN RAISE EXCEPTION 'TASK_RESULT_BASE_AMBIGUOUS';END IF;
 SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
 WHERE p.oid=target AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND oidvectortypes(p.proargtypes)='uuid, integer, bigint, uuid, text';
 IF source IS NULL OR strpos(source,'-- 02535:')>0
 OR strpos(source,'OUTPUT_NOT_VERIFIED')=0
 OR strpos(source,'END LOOP;'||chr(10)||marker)=0
 OR (length(source)-length(replace(source,marker,'')))/length(marker)<>1 THEN
  RAISE EXCEPTION 'TASK_RESULT_BASE_SOURCE_SHAPE';
 END IF;
 patched:=replace(source,marker,guard||marker);
 EXECUTE replace(definition,source,patched);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
