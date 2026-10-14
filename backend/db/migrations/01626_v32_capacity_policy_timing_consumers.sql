-- +goose Up
-- Private helpers use only operator-installed exact policy pins.
-- Epoch comparison avoids interval overflow for int64-safe policies.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_worker_fresh(p_worker photo.worker_registrations)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(CASE WHEN p_worker.capacity_policy_sha IS NULL THEN
  p_worker.last_seen<=clock_timestamp() AND p_worker.last_seen>clock_timestamp()-interval '30 seconds'
 ELSE EXISTS(SELECT 1 FROM photo.capacity_policy_timings c WHERE c.policy_sha=p_worker.capacity_policy_sha
  AND p_worker.reported_capacity_policy_sha=c.policy_sha
  AND p_worker.last_seen<=clock_timestamp() AND p_worker.capacity_policy_reported_at<=clock_timestamp()
  AND extract(epoch FROM clock_timestamp()-p_worker.last_seen)<c.offline_seconds
  AND extract(epoch FROM clock_timestamp()-p_worker.capacity_policy_reported_at)<c.offline_seconds) END,false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_worker_fresh(photo.worker_registrations) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_worker_fresh(photo.worker_registrations) FROM PUBLIC,photo_worker,photo_api,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_task_lease_seconds(p_tenant uuid,p_worker uuid,p_timeout integer)
RETURNS integer LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE duration integer;
BEGIN
 IF p_timeout IS NULL OR p_timeout<1 THEN RAISE EXCEPTION 'INVALID_TASK_TIMEOUT' USING ERRCODE='22023'; END IF;
 SELECT CASE WHEN w.capacity_policy_sha IS NULL THEN least(60,p_timeout)
 ELSE least(c.lease_seconds,p_timeout::bigint)::integer END INTO duration
 FROM photo.worker_registrations w LEFT JOIN photo.capacity_policy_timings c ON c.policy_sha=w.capacity_policy_sha
 WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled
 AND photo.capacity_policy_worker_fresh(w);
 -- NULL passed to least(timestamp,NULL) would silently select deadline.
 IF duration IS NULL THEN RAISE EXCEPTION 'CAPACITY_POLICY_TIMING_STALE' USING ERRCODE='40001'; END IF;
 RETURN duration;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_task_lease_seconds(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_task_lease_seconds(uuid,uuid,integer) FROM PUBLIC,photo_worker,photo_api,photo_scheduler;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.worker_authorized(p_tenant uuid,p_worker uuid,p_class text,p_node text,p_operation text,p_backend text,p_lock_id text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.worker_authorized_pre_v31_signer(p_tenant,p_worker,p_class,p_node,p_operation,p_backend,p_lock_id)
 AND EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker
 AND CASE WHEN p_operation='provenance.sign' THEN w.execution_pool='signer' AND w.signer_qualified_until>clock_timestamp()
 ELSE w.execution_pool='general' END AND photo.capacity_policy_worker_fresh(w));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) FROM PUBLIC,photo_worker,photo_api;
-- Preserve installed fairness/model/signer predicates, OIDs and ACLs.
-- +goose StatementBegin
DO $$
DECLARE signature text; definition text; expected integer; occurrences integer;
 needle text:='(w.capacity_policy_sha IS NULL OR (w.reported_capacity_policy_sha=w.capacity_policy_sha AND w.capacity_policy_reported_at>clock_timestamp()-interval ''30 seconds'' AND w.capacity_policy_reported_at<=clock_timestamp())) AND w.last_seen>clock_timestamp()-interval ''30 seconds''';
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.eligible_dispatch_nodes(uuid)',
 'photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
 'photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
 'photo.select_style_analysis_runtimes(uuid,boolean)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  expected:=CASE WHEN signature='photo.select_style_analysis_runtimes(uuid,boolean)' THEN 2 ELSE 1 END;
  occurrences:=(length(definition)-length(replace(definition,needle,'')))/length(needle);
  IF occurrences<>expected THEN RAISE EXCEPTION 'Unexpected capacity timing dispatch definition: %',signature; END IF;
  EXECUTE replace(definition,needle,'photo.capacity_policy_worker_fresh(w)');
 END LOOP;
END $$;
-- +goose StatementEnd
-- Patch the lease writer below all current public claim wrappers.
-- +goose StatementBegin
DO $$
DECLARE signature text; definition text; needle text:='clock_timestamp()+interval ''60 seconds''';
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.claim_task_reserved(uuid,integer,uuid,jsonb)',
 'photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'Unexpected capacity timing lease definition: %',signature;
  END IF;
  EXECUTE replace(definition,needle,'clock_timestamp()+make_interval(secs=>photo.capacity_policy_task_lease_seconds(t.tenant_id,p_worker,t.timeout_seconds))');
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
