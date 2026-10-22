-- +goose Up
-- Only authenticated training admissions use the typed V2 quota finalizer here.
-- This prevents generic cancellation from making their reservation unreachable.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_cancellation_scope(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.runs r
  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
   AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='learning' AND w.renderer_version='v2.2'
  JOIN photo.learning_profiles p ON p.tenant_id=r.tenant_id AND p.project_id=w.project_id
   AND p.id::text=r.config_snapshot->'v23_training'->>'profile_id'
  JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
   AND s.kind='v2' AND s.digest=r.config_snapshot->'v23_training'->>'snapshot_digest'
  JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
   AND (q.state='reserved' OR (q.state='released' AND q.bytes=0)) AND q.principal_type='user' AND q.principal_id=r.created_by
   AND q.files=0 AND q.share_id IS NULL
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
   AND r.status='cancelling' AND r.cancel_requested
   AND jsonb_typeof(r.config_snapshot->'v23_training')='object'
   AND r.config_snapshot->>'project_id'=w.project_id::text
   AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id)
   AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
    AND (t.operation NOT IN('learning.fit','learning.evaluate') OR t.operation_version<>'1'))
 )
$$;
ALTER FUNCTION photo.v23_training_cancellation_scope(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_cancellation_scope(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_maintenance,photo_gateway,photo_portal;
DO $$
DECLARE source text; definition text; before_catalog record; after_catalog record;
 declaration constant text:='DECLARE candidate uuid; r photo.runs%ROWTYPE; next_cursor bigint;';
 lock_anchor constant text:=' SELECT * INTO r FROM photo.runs WHERE id=candidate FOR UPDATE SKIP LOCKED;';
 recheck_anchor constant text:=' IF NOT FOUND OR NOT r.cancel_requested OR r.status<>''cancelling'' THEN RETURN 0;END IF;';
 finish_anchor constant text:=' INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(r.tenant_id,0)';
BEGIN
 SELECT oid,proowner,proacl,proconfig,prolang,provolatile,prorettype,proretset,pronargs,prokind
 INTO before_catalog FROM pg_proc WHERE oid='photo.finalize_run_cancellation()'::regprocedure;
 SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
 WHERE p.oid='photo.finalize_run_cancellation()'::regprocedure AND p.prosecdef
 AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
 AND p.pronargs=0 AND p.prokind='f' AND NOT p.proretset AND p.prorettype='integer'::regtype
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
  WHERE a.privilege_type<>'EXECUTE' OR a.grantee NOT IN(p.proowner,'photo_scheduler'::regrole::oid,'photo_maintenance'::regrole::oid))
 AND has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 AND has_function_privilege('photo_maintenance',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_gateway',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_portal',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_auth',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_publisher',p.oid,'EXECUTE');
 IF source IS NULL OR strpos(source,'02587')>0
 OR (length(source)-length(replace(source,declaration,'')))/length(declaration)<>1
 OR (length(source)-length(replace(source,lock_anchor,'')))/length(lock_anchor)<>1
 OR (length(source)-length(replace(source,recheck_anchor,'')))/length(recheck_anchor)<>1
 OR (length(source)-length(replace(source,finish_anchor,'')))/length(finish_anchor)<>1 THEN
  RAISE EXCEPTION 'V23_TRAINING_CANCEL_FINALIZER_SOURCE_SHAPE'; END IF;
 source:=replace(source,declaration,declaration||E'\n training_tenant uuid; training_cancel boolean:=false; typed_outcome text;');
 source:=replace(source,lock_anchor,$patch$
 -- 02587: match typed finalizer lock order before acquiring its run lock.
 SELECT x.tenant_id INTO training_tenant FROM photo.runs x WHERE x.id=candidate
  AND x.kind='v2' AND x.config_snapshot ? 'v23_training';
 IF training_tenant IS NOT NULL THEN
  PERFORM 1 FROM photo.tenants WHERE id=training_tenant FOR UPDATE;
  PERFORM 1 FROM photo.tenant_usage WHERE tenant_id=training_tenant FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V23_TRAINING_CANCEL_USAGE_MISSING' USING ERRCODE='23514';END IF;
 END IF;
$patch$||lock_anchor);
 source:=replace(source,recheck_anchor,recheck_anchor||$patch$
 IF training_tenant IS NOT NULL THEN
  IF r.tenant_id<>training_tenant OR NOT photo.v23_training_cancellation_scope(r.tenant_id,r.id) THEN
   RAISE EXCEPTION 'V23_TRAINING_CANCEL_SCOPE_INVALID' USING ERRCODE='23514';END IF;
  training_cancel:=true;
 END IF;
$patch$);
 source:=replace(source,finish_anchor,$patch$
 IF training_cancel THEN
  -- Existing checks settled every task/review and closed all expansions above.
  -- Allocated output bytes and backend documents retain their independent ledgers.
  UPDATE photo.runs SET graph_sealed=true WHERE tenant_id=r.tenant_id AND id=r.id;
  typed_outcome:=photo.finalize_v2_run(r.tenant_id,r.id);
  IF typed_outcome IS NULL OR typed_outcome NOT IN('cancelled','failed') THEN
   RAISE EXCEPTION 'V23_TRAINING_CANCEL_TYPED_FINALIZATION_REFUSED' USING ERRCODE='23514';END IF;
  RETURN 1;
 END IF;
$patch$||finish_anchor);
 EXECUTE replace(definition,(SELECT prosrc FROM pg_proc WHERE oid='photo.finalize_run_cancellation()'::regprocedure),source);
 SELECT oid,proowner,proacl,proconfig,prolang,provolatile,prorettype,proretset,pronargs,prokind
 INTO after_catalog FROM pg_proc WHERE oid='photo.finalize_run_cancellation()'::regprocedure;
 IF after_catalog IS DISTINCT FROM before_catalog THEN
  RAISE EXCEPTION 'V23_TRAINING_CANCEL_FINALIZER_AUTHORITY_CHANGED'; END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
