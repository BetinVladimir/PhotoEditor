-- +goose Up
-- Only operator/owner can call this; no runtime role receives EXECUTE.
-- +goose StatementBegin
CREATE FUNCTION photo.rotate_worker_capacity_policy(p_worker uuid,p_principal name,p_node text,p_expected text,p_next text)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE changed integer;
BEGIN
 IF p_worker IS NULL OR p_principal IS NULL OR p_node IS NULL OR p_node=''
 OR p_next IS NULL OR p_next !~ '^[0-9a-f]{64}$'
 OR (p_expected IS NOT NULL AND p_expected !~ '^[0-9a-f]{64}$') THEN
 RAISE EXCEPTION 'CAPACITY_POLICY_ROTATION_INVALID' USING ERRCODE='22023'; END IF;
 PERFORM policy_sha FROM photo.capacity_policy_timings WHERE policy_sha=p_next FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'CAPACITY_POLICY_NOT_INSTALLED' USING ERRCODE='23514'; END IF;
 -- Match capacity heartbeat's lock order: timing -> node -> registrations.
 PERFORM node_id FROM photo.resource_nodes WHERE node_id=p_node AND enabled FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'CAPACITY_POLICY_NODE_UNAVAILABLE' USING ERRCODE='40001'; END IF;
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=p_principal AND enabled ORDER BY id FOR UPDATE;
 IF NOT FOUND OR EXISTS(SELECT 1 FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=p_principal AND enabled
  AND (node_id IS DISTINCT FROM p_node OR capacity_policy_sha IS DISTINCT FROM p_expected)) THEN
 RAISE EXCEPTION 'CAPACITY_POLICY_ROTATION_CONFLICT' USING ERRCODE='40001'; END IF;
 -- Retrying an already installed pin must not invalidate a new report.
 IF p_expected IS NOT DISTINCT FROM p_next THEN
  SELECT count(*)::integer INTO changed FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=p_principal AND enabled;
  RETURN changed;
 END IF;
 UPDATE photo.worker_registrations SET capacity_policy_sha=p_next,reported_capacity_policy_sha=NULL,capacity_policy_reported_at=NULL
 WHERE worker_id=p_worker AND db_principal=p_principal AND enabled;
 GET DIAGNOSTICS changed=ROW_COUNT;
 RETURN changed;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.rotate_worker_capacity_policy(uuid,name,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.rotate_worker_capacity_policy(uuid,name,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
-- Exclusive registration locks serialize lease writers and CAS rotation.
-- Lease writers also refresh last_seen: FOR SHARE would deadlock on concurrent lock upgrades.
-- No slot/usage/run/task locks are taken by rotation, avoiding a reverse dependency.
-- +goose StatementBegin
DO $$
DECLARE signature text; definition text; needle text:='t:=photo.lock_task(p_task);';
 gate text:='
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND tenant_id=t.tenant_id
 AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;';
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.claim_task_reserved(uuid,integer,uuid,jsonb)',
 'photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'Unexpected capacity rotation lease definition: %',signature;
  END IF;
  EXECUTE replace(definition,needle,needle||gate);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
