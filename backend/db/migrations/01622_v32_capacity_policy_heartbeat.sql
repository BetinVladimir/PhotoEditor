-- +goose Up
ALTER TABLE photo.worker_registrations ADD COLUMN capacity_policy_sha text CHECK (capacity_policy_sha IS NULL OR capacity_policy_sha ~ '^[0-9a-f]{64}$');
ALTER TABLE photo.worker_registrations ADD COLUMN reported_capacity_policy_sha text CHECK (reported_capacity_policy_sha IS NULL OR reported_capacity_policy_sha ~ '^[0-9a-f]{64}$');
ALTER TABLE photo.worker_registrations ADD COLUMN capacity_policy_reported_at timestamptz;
-- Internal implementation is callable only by its SECURITY DEFINER owner.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_heartbeat_cpu_worker_locked(p_worker uuid,p_class text,p_operations text[],p_ram bigint,p_scratch bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node text; node_count integer; gpu_budget bigint;
BEGIN
 IF p_class IS NULL OR p_class NOT IN ('cpu','media','archive') OR p_operations IS NULL OR array_position(p_operations,NULL) IS NOT NULL OR cardinality(p_operations) NOT BETWEEN 1 AND 128
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
ALTER FUNCTION photo.capacity_heartbeat_cpu_worker_locked(uuid,text,text[],bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_heartbeat_cpu_worker_locked(uuid,text,text[],bigint,bigint) FROM PUBLIC,photo_worker;
-- Legacy reports cannot refresh policy-pinned registrations.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.heartbeat_cpu_worker(p_worker uuid,p_class text,p_operations text[],p_ram bigint,p_scratch bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node text; node_count integer; gpu_budget bigint;
BEGIN
 IF p_class IS NULL OR p_class NOT IN ('cpu','media','archive') OR p_operations IS NULL OR array_position(p_operations,NULL) IS NOT NULL OR cardinality(p_operations) NOT BETWEEN 1 AND 128
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
 IF EXISTS(SELECT FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=session_user AND enabled AND capacity_policy_sha IS NOT NULL) THEN RETURN false; END IF;
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
-- Internal implementation is callable only by its SECURITY DEFINER owner.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_heartbeat_accelerator_worker_locked(p_worker uuid,p_backend text,p_operations text[],p_ram bigint,p_vram bigint,p_scratch bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node text; node_count integer; gpu_budget bigint;
BEGIN
 IF p_backend IS NULL OR p_backend NOT IN ('cpu','mps','cuda')
 OR p_operations IS NULL OR array_position(p_operations,NULL) IS NOT NULL OR cardinality(p_operations) NOT BETWEEN 1 AND 128
 OR p_ram IS NULL OR p_ram NOT BETWEEN 0 AND 9007199254740991
 OR p_vram IS NULL OR p_vram NOT BETWEEN 0 AND 9007199254740991
 OR p_scratch IS NULL OR p_scratch NOT BETWEEN 0 AND 9007199254740991
 OR (p_backend='cpu' AND p_vram<>0) THEN RAISE EXCEPTION 'INVALID_WORKER_REPORT' USING ERRCODE='22023'; END IF;
 SELECT count(DISTINCT w.node_id),min(w.node_id) INTO node_count,node FROM photo.worker_registrations w
 WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled;
 IF node_count<>1 OR node IS NULL THEN RETURN false; END IF;
 SELECT vram_budget_bytes INTO gpu_budget FROM photo.resource_nodes
 WHERE node_id=node AND enabled AND doctor_report_sha256 IS NOT NULL FOR UPDATE;
 IF NOT FOUND OR (p_backend='cpu' AND gpu_budget<>0) OR (p_backend<>'cpu' AND gpu_budget=0) THEN RETURN false; END IF;
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 IF EXISTS(SELECT FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled
 AND (w.node_id IS DISTINCT FROM node OR w.resource_classes<>'["gpu"]'::jsonb
 OR w.backends<>jsonb_build_array(p_backend) OR w.certificate_sha256 IS NULL)) THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_runtime_operations(p_worker,'gpu',p_backend)) OR EXISTS(
 SELECT FROM photo.worker_runtime_operations(p_worker,'gpu',p_backend) op WHERE NOT(op.operation=ANY(p_operations))) THEN RETURN false; END IF;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp()
 WHERE worker_id=p_worker AND db_principal=session_user AND enabled AND node_id=node
 AND resource_classes='["gpu"]'::jsonb AND backends=jsonb_build_array(p_backend) AND certificate_sha256 IS NOT NULL;
 IF NOT FOUND THEN RETURN false; END IF;
 UPDATE photo.resource_nodes SET ram_available_bytes=least(p_ram,ram_budget_bytes),vram_available_bytes=least(p_vram,vram_budget_bytes),
 scratch_available_bytes=least(p_scratch,scratch_budget_bytes),reported_at=clock_timestamp() WHERE node_id=node;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_heartbeat_accelerator_worker_locked(uuid,text,text[],bigint,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_heartbeat_accelerator_worker_locked(uuid,text,text[],bigint,bigint,bigint) FROM PUBLIC,photo_worker;
-- Legacy reports cannot refresh policy-pinned registrations.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.heartbeat_accelerator_worker(p_worker uuid,p_backend text,p_operations text[],p_ram bigint,p_vram bigint,p_scratch bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node text; node_count integer; gpu_budget bigint;
BEGIN
 IF p_backend IS NULL OR p_backend NOT IN ('cpu','mps','cuda')
 OR p_operations IS NULL OR array_position(p_operations,NULL) IS NOT NULL OR cardinality(p_operations) NOT BETWEEN 1 AND 128
 OR p_ram IS NULL OR p_ram NOT BETWEEN 0 AND 9007199254740991
 OR p_vram IS NULL OR p_vram NOT BETWEEN 0 AND 9007199254740991
 OR p_scratch IS NULL OR p_scratch NOT BETWEEN 0 AND 9007199254740991
 OR (p_backend='cpu' AND p_vram<>0) THEN RAISE EXCEPTION 'INVALID_WORKER_REPORT' USING ERRCODE='22023'; END IF;
 SELECT count(DISTINCT w.node_id),min(w.node_id) INTO node_count,node FROM photo.worker_registrations w
 WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled;
 IF node_count<>1 OR node IS NULL THEN RETURN false; END IF;
 SELECT vram_budget_bytes INTO gpu_budget FROM photo.resource_nodes
 WHERE node_id=node AND enabled AND doctor_report_sha256 IS NOT NULL FOR UPDATE;
 IF NOT FOUND OR (p_backend='cpu' AND gpu_budget<>0) OR (p_backend<>'cpu' AND gpu_budget=0) THEN RETURN false; END IF;
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 IF EXISTS(SELECT FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled
 AND (w.node_id IS DISTINCT FROM node OR w.resource_classes<>'["gpu"]'::jsonb
 OR w.backends<>jsonb_build_array(p_backend) OR w.certificate_sha256 IS NULL)) THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_runtime_operations(p_worker,'gpu',p_backend)) OR EXISTS(
 SELECT FROM photo.worker_runtime_operations(p_worker,'gpu',p_backend) op WHERE NOT(op.operation=ANY(p_operations))) THEN RETURN false; END IF;
 IF EXISTS(SELECT FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=session_user AND enabled AND capacity_policy_sha IS NOT NULL) THEN RETURN false; END IF;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp()
 WHERE worker_id=p_worker AND db_principal=session_user AND enabled AND node_id=node
 AND resource_classes='["gpu"]'::jsonb AND backends=jsonb_build_array(p_backend) AND certificate_sha256 IS NOT NULL;
 IF NOT FOUND THEN RETURN false; END IF;
 UPDATE photo.resource_nodes SET ram_available_bytes=least(p_ram,ram_budget_bytes),vram_available_bytes=least(p_vram,vram_budget_bytes),
 scratch_available_bytes=least(p_scratch,scratch_budget_bytes),reported_at=clock_timestamp() WHERE node_id=node;
 RETURN true;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.heartbeat_worker_capacity_v2(p_worker uuid,p_class text,p_backend text,p_operations text[],p_ram bigint,p_vram bigint,p_scratch bigint,p_policy_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node text; nodes integer; accepted boolean;
BEGIN
 IF p_policy_sha IS NULL OR p_policy_sha !~ '^[0-9a-f]{64}$'
 OR p_class IS NULL OR p_class NOT IN ('cpu','media','archive','gpu')
 OR p_backend IS NULL OR p_backend NOT IN ('cpu','mps','cuda')
 OR (p_class<>'gpu' AND (p_backend<>'cpu' OR p_vram IS DISTINCT FROM 0::bigint)) THEN
 RAISE EXCEPTION 'INVALID_CAPACITY_POLICY_REPORT' USING ERRCODE='22023'; END IF;
 SELECT count(DISTINCT w.node_id),min(w.node_id) INTO nodes,node FROM photo.worker_registrations w
 WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled;
 IF nodes<>1 OR node IS NULL THEN RETURN false; END IF;
 PERFORM node_id FROM photo.resource_nodes WHERE node_id=node AND enabled FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 IF NOT FOUND OR EXISTS(SELECT FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=session_user AND enabled
 AND (node_id IS DISTINCT FROM node OR capacity_policy_sha IS DISTINCT FROM p_policy_sha)) THEN RETURN false; END IF;
 IF p_class='gpu' THEN
 accepted:=photo.capacity_heartbeat_accelerator_worker_locked(p_worker,p_backend,p_operations,p_ram,p_vram,p_scratch);
 ELSE
 accepted:=photo.capacity_heartbeat_cpu_worker_locked(p_worker,p_class,p_operations,p_ram,p_scratch);
 END IF;
 IF accepted IS NOT TRUE THEN RETURN false; END IF;
 UPDATE photo.worker_registrations SET reported_capacity_policy_sha=p_policy_sha,capacity_policy_reported_at=clock_timestamp()
 WHERE worker_id=p_worker AND db_principal=session_user AND enabled AND node_id=node AND capacity_policy_sha=p_policy_sha;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.heartbeat_worker_capacity_v2(uuid,text,text,text[],bigint,bigint,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.heartbeat_worker_capacity_v2(uuid,text,text,text[],bigint,bigint,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.heartbeat_worker_capacity_v2(uuid,text,text,text[],bigint,bigint,bigint,text) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
