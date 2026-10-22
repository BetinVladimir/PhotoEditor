-- Operator-only initial activation after reviewing real target-host evidence.
-- Required psql variables: tenant_id, worker_id, node_id, db_principal,
-- model_lock_sha, certificate_sha, capacity_policy_sha,
-- doctor_report_jcs, doctor_report_file_sha, qualified_operations_json.
-- doctor_report_file_sha hashes exact canonical file bytes including report_sha256.
-- Operation qualification remains an explicit operator-reviewed allowlist;
-- model doctor smoke is NOT photo-quality qualification.
\set ON_ERROR_STOP on
BEGIN;
CREATE TEMP TABLE rtx_activation_input ON COMMIT DROP AS
SELECT :'tenant_id'::uuid tenant_id, :'worker_id'::uuid worker_id,
 :'node_id'::text node_id, :'db_principal'::name db_principal,
 :'model_lock_sha'::text model_lock_sha, :'certificate_sha'::text certificate_sha,
 :'capacity_policy_sha'::text capacity_policy_sha,
 :'doctor_report_jcs'::text report_raw, :'doctor_report_file_sha'::text report_sha,
 :'qualified_operations_json'::jsonb operations;
DO $$
DECLARE input record; registration photo.worker_registrations; node photo.resource_nodes;
BEGIN
 SELECT * INTO STRICT input FROM rtx_activation_input;
 PERFORM policy_sha FROM photo.capacity_policy_timings WHERE policy_sha=input.capacity_policy_sha FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'RTX_ACTIVATION_POLICY_MISSING'; END IF;
 -- Recheck the login: grants can change after disabled registration.
 IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname=input.db_principal
  AND rolcanlogin AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication)
 OR NOT pg_has_role(input.db_principal,'photo_worker','MEMBER')
 OR EXISTS(SELECT FROM pg_roles privileged
  WHERE (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreaterole OR privileged.rolreplication
   OR privileged.rolname IN ('photo_fn_owner','photo_runtime_owner')
   OR EXISTS(SELECT FROM pg_namespace n WHERE n.nspname='photo' AND n.nspowner=privileged.oid)
   OR EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
      WHERE n.nspname='photo' AND c.relowner=privileged.oid)
   OR EXISTS(SELECT FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
      WHERE n.nspname='photo' AND p.prosecdef AND p.proowner=privileged.oid))
   AND pg_has_role(input.db_principal,privileged.oid,'MEMBER'))
 OR has_schema_privilege(input.db_principal,'photo','CREATE')
 OR EXISTS(SELECT FROM pg_roles role WHERE role.rolname IN ('photo_api','photo_scheduler','photo_gateway','photo_portal','photo_fn_owner',
                     'photo_semantic_admission','pg_read_all_data','pg_write_all_data',
                     'pg_execute_server_program','pg_read_server_files','pg_write_server_files')
  AND pg_has_role(input.db_principal,role.rolname,'MEMBER')) THEN
  RAISE EXCEPTION 'RTX_ACTIVATION_LOGIN_PRIVILEGES_INVALID';
 END IF;
 -- Initial activation only: never overwrite a running, drained or shared node.
 SELECT * INTO STRICT node FROM photo.resource_nodes WHERE node_id=input.node_id FOR UPDATE;
 SELECT * INTO STRICT registration FROM photo.worker_registrations
 WHERE tenant_id=input.tenant_id AND worker_id=input.worker_id FOR UPDATE;
 IF node.enabled OR node.drain_generation<>0 OR node.drain_started_at IS NOT NULL
 OR node.doctor_report_sha256 IS NOT NULL OR node.vram_budget_bytes<>10000000000
 OR registration.enabled OR registration.node_id IS DISTINCT FROM input.node_id
 OR registration.db_principal IS DISTINCT FROM input.db_principal
 OR registration.lock_id IS DISTINCT FROM input.model_lock_sha
 OR registration.certificate_sha256 IS DISTINCT FROM input.certificate_sha
 OR registration.capacity_policy_sha IS DISTINCT FROM input.capacity_policy_sha
 OR registration.resource_classes<>'["gpu"]'::jsonb OR registration.backends<>'["cuda"]'::jsonb
 OR EXISTS(SELECT FROM photo.worker_registrations WHERE node_id=input.node_id AND id<>registration.id)
 OR EXISTS(SELECT FROM photo.resource_slots WHERE node_id=input.node_id) THEN
  RAISE EXCEPTION 'RTX_ACTIVATION_IDENTITY_CONFLICT';
 END IF;
 IF input.report_sha !~ '^[0-9a-f]{64}$' OR octet_length(input.report_raw) NOT BETWEEN 2 AND 1048576
 OR encode(sha256(convert_to(input.report_raw,'UTF8')),'hex')<>input.report_sha
 OR (input.report_raw::jsonb)->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (input.report_raw::jsonb)->>'backend' IS DISTINCT FROM 'cuda'
 OR (input.report_raw::jsonb)->>'lock_id' IS DISTINCT FROM registration.lock_id
 OR jsonb_typeof((input.report_raw::jsonb)->'models') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'RTX_ACTIVATION_DOCTOR_REPORT_INVALID';
 END IF;
 IF EXISTS(SELECT FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
  WHERE jsonb_typeof(model) IS DISTINCT FROM 'object'
   OR jsonb_typeof(model->'model_id') IS DISTINCT FROM 'string'
   OR length(coalesce(model->>'model_id','')) NOT BETWEEN 1 AND 256
   OR NOT coalesce(model->>'status'=ANY(ARRAY['passed','failed','unsupported']),false))
 OR EXISTS(SELECT model->>'model_id' FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
  GROUP BY model->>'model_id' HAVING count(*)<>1) THEN
  RAISE EXCEPTION 'RTX_ACTIVATION_DOCTOR_REPORT_INVALID';
 END IF;
 IF jsonb_typeof(input.operations) IS DISTINCT FROM 'object' THEN
  RAISE EXCEPTION 'RTX_ACTIVATION_OPERATION_INVENTORY_INVALID';
 END IF;
 IF (SELECT count(*) FROM jsonb_each(input.operations)) NOT BETWEEN 1 AND 128
 OR EXISTS(SELECT FROM jsonb_each(input.operations) item
  LEFT JOIN photo.operation_registry op ON op.operation=item.key AND op.version='1'
  WHERE item.value<>'["cuda"]'::jsonb OR op.operation IS NULL
   OR op.resource_class<>'gpu' OR NOT(op.backends ? 'cuda')
   OR (op.model_id IS NOT NULL AND NOT EXISTS(
    SELECT FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
    WHERE model->>'model_id'=op.model_id AND model->>'status'='passed'
     AND model->>'lock_backend_status'='passed'))) THEN
  RAISE EXCEPTION 'RTX_ACTIVATION_OPERATION_UNQUALIFIED';
 END IF;
END $$;
UPDATE photo.resource_nodes n SET enabled=true,doctor_report_sha256=i.report_sha,
 reported_at=NULL,ram_available_bytes=0,vram_available_bytes=0,scratch_available_bytes=0
FROM rtx_activation_input i WHERE n.node_id=i.node_id;
UPDATE photo.worker_registrations w SET enabled=true,qualified_operations=i.operations,
 last_seen='epoch',reported_capacity_policy_sha=NULL,capacity_policy_reported_at=NULL
FROM rtx_activation_input i WHERE w.tenant_id=i.tenant_id AND w.worker_id=i.worker_id;
INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity)
SELECT node_id,'gpu1','gpu','{}'::jsonb FROM rtx_activation_input;
COMMIT;
-- Availability remains zero/stale until the authenticated worker heartbeat.
-- Existing project/operation quality, provenance and admission gates stay active.
