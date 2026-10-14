-- +goose Up
ALTER TABLE photo.resource_nodes
 ADD COLUMN capacity_report_worker_id uuid,
 ADD COLUMN capacity_report_policy_sha text,
 ADD CONSTRAINT capacity_report_policy_binding CHECK(
  (capacity_report_worker_id IS NULL AND capacity_report_policy_sha IS NULL) OR
  (capacity_report_worker_id IS NOT NULL AND capacity_report_policy_sha IS NOT NULL
   AND capacity_report_policy_sha ~ '^[0-9a-f]{64}$'));
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_node_fresh(p_node photo.resource_nodes)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_node.reported_at<=clock_timestamp() AND CASE
 WHEN p_node.capacity_report_worker_id IS NULL THEN p_node.reported_at>clock_timestamp()-interval '30 seconds'
 ELSE EXISTS(SELECT 1 FROM photo.capacity_policy_timings c JOIN photo.worker_registrations w
  ON w.capacity_policy_sha=c.policy_sha AND w.worker_id=p_node.capacity_report_worker_id
  WHERE c.policy_sha=p_node.capacity_report_policy_sha AND w.node_id=p_node.node_id AND w.enabled
  AND photo.capacity_policy_worker_fresh(w)
  AND extract(epoch FROM clock_timestamp()-p_node.reported_at)<c.offline_seconds) END,false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_node_fresh(photo.resource_nodes) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_node_fresh(photo.resource_nodes) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
-- Actual low-level report writers first clear legacy source identity. The v2
-- wrapper then binds the source in the same transaction only when it updated capacity.
-- +goose StatementBegin
DO $$
DECLARE signature text; definition text; needle text:='UPDATE photo.resource_nodes SET ram_available_bytes=';
BEGIN
 FOREACH signature IN ARRAY ARRAY[
 'photo.heartbeat_cpu_worker(uuid,text,text[],bigint,bigint)',
 'photo.heartbeat_accelerator_worker(uuid,text,text[],bigint,bigint,bigint)',
 'photo.capacity_heartbeat_cpu_worker_locked(uuid,text,text[],bigint,bigint)',
 'photo.capacity_heartbeat_accelerator_worker_locked(uuid,text,text[],bigint,bigint,bigint)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'Unexpected node capacity report writer: %',signature;
  END IF;
  EXECUTE replace(definition,needle,'UPDATE photo.resource_nodes SET capacity_report_worker_id=NULL,capacity_report_policy_sha=NULL,ram_available_bytes=');
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text:=' IF accepted IS NOT TRUE THEN RETURN false; END IF;';
 gate text:='
 UPDATE photo.resource_nodes SET capacity_report_worker_id=p_worker,capacity_report_policy_sha=p_policy_sha
 WHERE node_id=node AND (p_class=''gpu'' OR vram_budget_bytes=0);';
BEGIN
 definition:=pg_get_functiondef('photo.heartbeat_worker_capacity_v2(uuid,text,text,text[],bigint,bigint,bigint,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected policy capacity report writer';
 END IF;
 EXECUTE replace(definition,needle,needle||gate);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE signature text; definition text; needle text; replacement text;
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.eligible_dispatch_nodes(uuid)',
 'photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
 'photo.tenant_capabilities()','photo.tenant_capabilities_evidence()'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF signature='photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)' THEN
   needle:='n.reported_at IS NULL OR n.reported_at<clock_timestamp()-interval ''30 seconds''';
   replacement:='NOT photo.capacity_policy_node_fresh(n)';
  ELSE
   needle:=CASE WHEN signature='photo.eligible_dispatch_nodes(uuid)' THEN
    'n.reported_at>clock_timestamp()-interval ''30 seconds''' ELSE
    'n.reported_at>current_timestamp-interval ''30 seconds''' END;
   replacement:='photo.capacity_policy_node_fresh(n)';
  END IF;
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'Unexpected capacity node freshness definition: %',signature;
  END IF;
  definition:=replace(definition,needle,replacement);
  IF signature IN('photo.tenant_capabilities()','photo.tenant_capabilities_evidence()') THEN
   needle:='w.last_seen>current_timestamp-interval ''30 seconds''';
   IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
    RAISE EXCEPTION 'Unexpected capability worker freshness definition: %',signature;
   END IF;
   definition:=replace(definition,needle,'photo.capacity_policy_worker_fresh(w)');
  END IF;
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
