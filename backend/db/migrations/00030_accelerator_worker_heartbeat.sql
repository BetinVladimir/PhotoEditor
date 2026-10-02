-- +goose Up
-- Legacy unrestricted reports must no longer refresh a mixed/GPU node.
REVOKE EXECUTE ON FUNCTION photo.report_worker_capacity(uuid,uuid,bigint,bigint,bigint) FROM photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.worker_runtime_operations(p_worker uuid,p_class text,p_backend text)
RETURNS TABLE(operation text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT DISTINCT op.operation FROM photo.worker_registrations w
 JOIN photo.resource_nodes n ON n.node_id=w.node_id
 JOIN photo.operation_registry op ON op.resource_class=p_class AND op.version='1'
 WHERE p_class='gpu' AND p_backend IN ('cpu','mps','cuda')
 AND w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled AND n.enabled
 AND w.resource_classes=jsonb_build_array(p_class) AND w.backends=jsonb_build_array(p_backend)
 AND op.backends ? p_backend AND (w.qualified_operations->op.operation) ? p_backend
 AND w.certificate_sha256 IS NOT NULL AND n.doctor_report_sha256 IS NOT NULL
 ORDER BY op.operation LIMIT 129;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.worker_runtime_operations(uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.worker_runtime_operations(uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.worker_runtime_operations(uuid,text,text) TO photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.heartbeat_accelerator_worker(p_worker uuid,p_backend text,p_operations text[],p_ram bigint,p_vram bigint,p_scratch bigint)
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
ALTER FUNCTION photo.heartbeat_accelerator_worker(uuid,text,text[],bigint,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.heartbeat_accelerator_worker(uuid,text,text[],bigint,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.heartbeat_accelerator_worker(uuid,text,text[],bigint,bigint,bigint) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
