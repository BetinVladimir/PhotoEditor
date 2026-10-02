-- +goose Up
-- CPU runtimes report host RAM/scratch only. They must not fabricate GPU capacity
-- or refresh an old GPU measurement on a mixed node.
-- +goose StatementBegin
CREATE FUNCTION photo.heartbeat_cpu_worker(p_worker uuid,p_class text,p_operations text[],p_ram bigint,p_scratch bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node text; node_count integer; gpu_budget bigint;
BEGIN
 IF p_class IS NULL OR p_class NOT IN ('cpu','media') OR p_operations IS NULL OR array_position(p_operations,NULL) IS NOT NULL OR cardinality(p_operations) NOT BETWEEN 1 AND 128
 OR p_ram IS NULL OR p_ram<0 OR p_ram>9007199254740991 OR p_scratch IS NULL OR p_scratch<0 OR p_scratch>9007199254740991 THEN
 RAISE EXCEPTION 'INVALID_WORKER_REPORT' USING ERRCODE='22023'; END IF;
 SELECT count(DISTINCT w.node_id),min(w.node_id) INTO node_count,node FROM photo.worker_registrations w
 WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled;
 IF node_count<>1 OR node IS NULL THEN RETURN false; END IF;
 IF EXISTS(SELECT FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled
 AND (w.resource_classes<>jsonb_build_array(p_class) OR w.certificate_sha256 IS NULL)) THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_runtime_operations(p_worker,p_class)) OR EXISTS(
 SELECT FROM photo.worker_runtime_operations(p_worker,p_class) op WHERE NOT(op.operation=ANY(p_operations))) THEN RETURN false; END IF;
 SELECT vram_budget_bytes INTO gpu_budget FROM photo.resource_nodes WHERE node_id=node AND enabled FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 -- Recheck registration under row locks after acquiring the physical node.
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 IF EXISTS(SELECT FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled
 AND (w.node_id IS DISTINCT FROM node OR w.resource_classes<>jsonb_build_array(p_class) OR w.certificate_sha256 IS NULL)) THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_runtime_operations(p_worker,p_class)) OR EXISTS(
 SELECT FROM photo.worker_runtime_operations(p_worker,p_class) op WHERE NOT(op.operation=ANY(p_operations))) THEN RETURN false; END IF;
 -- lock order node -> worker registrations, as in dispatch and task heartbeat.
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp()
 WHERE worker_id=p_worker AND db_principal=session_user AND enabled AND node_id=node
 AND resource_classes=jsonb_build_array(p_class) AND certificate_sha256 IS NOT NULL;
 IF NOT FOUND THEN RETURN false; END IF;
 IF gpu_budget=0 THEN
  UPDATE photo.resource_nodes SET ram_available_bytes=least(p_ram,ram_budget_bytes),vram_available_bytes=0,
   scratch_available_bytes=least(p_scratch,scratch_budget_bytes),reported_at=clock_timestamp() WHERE node_id=node;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.heartbeat_cpu_worker(uuid,text,text[],bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.heartbeat_cpu_worker(uuid,text,text[],bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.heartbeat_cpu_worker(uuid,text,text[],bigint,bigint) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
