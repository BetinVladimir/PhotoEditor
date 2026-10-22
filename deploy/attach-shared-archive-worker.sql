-- Operator-only existing CPU-node attachment; NO migration/function/grant changes.
-- One reviewed JSON psql variable shared_archive_json. Never execute as worker login.
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='30s';
CREATE TEMP TABLE shared_archive_attach_input ON COMMIT DROP AS
SELECT payload,(payload->'expected_cpu'->>'tenant_id')::uuid tenant_id,
 (payload->'expected_cpu'->>'worker_id')::uuid cpu_id,
 payload->'expected_cpu'->>'node_id' node_id,
 (payload->'archive'->>'worker_id')::uuid archive_id,
 (payload->'archive'->>'db_principal')::name db_principal,
 payload->'archive'->>'certificate_sha256' certificate_sha,
 payload->'archive'->>'lock_id' lock_id,
 payload->>'doctor_report_jcs' report_raw,payload->>'doctor_report_file_sha' report_sha
FROM (SELECT :'shared_archive_json'::jsonb payload) q;
DO $$
DECLARE input record;cpu photo.worker_registrations;node photo.resource_nodes;member photo.worker_registrations;
 slots jsonb;policy photo.capacity_policy_timings;proof jsonb;
BEGIN
 SELECT * INTO STRICT input FROM shared_archive_attach_input;
 IF jsonb_typeof(input.payload->'expected_cpu') IS DISTINCT FROM 'object'
 OR jsonb_typeof(input.payload->'expected_node') IS DISTINCT FROM 'object'
 OR jsonb_typeof(input.payload->'expected_slots') IS DISTINCT FROM 'array'
 OR jsonb_typeof(input.payload->'archive') IS DISTINCT FROM 'object'
 OR input.cpu_id IS NULL OR input.archive_id IS NULL OR input.cpu_id=input.archive_id
 OR input.archive_id='00000000-0000-0000-0000-000000000000'::uuid
 OR input.db_principal IS NULL OR length(input.db_principal::text) NOT BETWEEN 1 AND 63
 OR NOT coalesce(input.certificate_sha ~ '^[0-9a-f]{64}$',false)
 OR input.lock_id IS DISTINCT FROM 'e3c7b8502ee0d2b532f3a83de0f179e72179492104881c234c91ce1ccfb747be' THEN
 RAISE EXCEPTION 'SHARED_ARCHIVE_INPUT_INVALID';END IF;
 SELECT * INTO STRICT node FROM photo.resource_nodes WHERE node_id=input.node_id FOR UPDATE;
 PERFORM id FROM photo.worker_registrations WHERE node_id=input.node_id ORDER BY id FOR UPDATE;
 SELECT * INTO STRICT cpu FROM photo.worker_registrations WHERE tenant_id=input.tenant_id AND worker_id=input.cpu_id;
 PERFORM id FROM photo.resource_slots WHERE node_id=input.node_id ORDER BY slot_name FOR UPDATE;
 SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY s.slot_name),'[]'::jsonb) INTO slots FROM photo.resource_slots s WHERE node_id=input.node_id;
 IF to_jsonb(node) IS DISTINCT FROM input.payload->'expected_node'
 OR to_jsonb(cpu) IS DISTINCT FROM input.payload->'expected_cpu'
 OR slots IS DISTINCT FROM input.payload->'expected_slots'
 OR NOT node.enabled OR NOT cpu.enabled OR node.drain_started_at IS NOT NULL
 OR node.ram_budget_bytes<>8589934592 OR node.vram_budget_bytes<>0 OR node.scratch_budget_bytes<1
 OR cpu.node_id IS DISTINCT FROM node.node_id OR cpu.resource_classes<>'["cpu"]'::jsonb OR cpu.backends<>'["cpu"]'::jsonb
 OR cpu.db_principal IS NULL OR cpu.db_principal=input.db_principal
 OR cpu.certificate_sha256 IS NULL OR cpu.certificate_sha256=input.certificate_sha
 OR cpu.lock_id IS DISTINCT FROM input.lock_id OR cpu.qualified_operations='{}'::jsonb
 OR NOT coalesce(node.doctor_report_sha256 ~ '^[0-9a-f]{64}$',false)
 OR EXISTS(SELECT FROM photo.resource_slots WHERE node_id=input.node_id AND
 (resource_class<>'cpu' OR task_id IS NOT NULL OR tenant_id IS NOT NULL OR fencing_token IS NOT NULL OR lease_until IS NOT NULL))
 OR EXISTS(SELECT FROM photo.resource_reservations WHERE node_id=input.node_id AND state='reserved')
 OR EXISTS(SELECT FROM photo.tasks WHERE state IN('queued','running') AND
 (worker_id IN(input.cpu_id,input.archive_id) OR dispatch_worker_id IN(input.cpu_id,input.archive_id))) THEN
 RAISE EXCEPTION 'SHARED_ARCHIVE_CAS_OR_IDLE_CONFLICT';END IF;
 SELECT * INTO STRICT policy FROM photo.capacity_policy_timings WHERE policy_sha=cpu.capacity_policy_sha FOR SHARE;
 IF encode(sha256(convert_to(policy.policy_jcs,'UTF8')),'hex')<>cpu.capacity_policy_sha
 OR (policy.policy_jcs::jsonb->>'control_ram_budget_bytes')::bigint<>node.ram_budget_bytes
 OR (policy.policy_jcs::jsonb->>'photo_ram_budget_bytes')::bigint<>node.ram_budget_bytes
 OR jsonb_array_length(slots)<>(policy.policy_jcs::jsonb->>'cpu_concurrency')::integer THEN
 RAISE EXCEPTION 'SHARED_ARCHIVE_POLICY_OR_SLOT_CONFLICT';END IF;
 proof:=input.payload->'preflight';
 IF jsonb_typeof(proof) IS DISTINCT FROM 'object' OR proof->>'status' IS DISTINCT FROM 'OPERATOR_REVIEWED_SHARED_PARENT'
 OR (proof->>'reviewed_at')::timestamptz IS NULL OR (proof->>'reviewed_at')::timestamptz NOT BETWEEN clock_timestamp()-interval '10 minutes' AND clock_timestamp()
 OR proof->>'cpu_worker_id' IS DISTINCT FROM input.cpu_id::text
 OR proof->>'archive_worker_id' IS DISTINCT FROM input.archive_id::text
 OR proof->>'policy_sha256' IS DISTINCT FROM cpu.capacity_policy_sha
 OR proof->'cpu_stopped' IS DISTINCT FROM 'true'::jsonb OR proof->'archive_stopped' IS DISTINCT FROM 'true'::jsonb
 OR proof->'scheduler_stopped' IS DISTINCT FROM 'true'::jsonb OR proof->'rabbit_empty' IS DISTINCT FROM 'true'::jsonb
 OR proof->'shared_parent_kernel_reviewed' IS DISTINCT FROM 'true'::jsonb
 OR proof->'parent_memory_max_bytes' IS DISTINCT FROM '8589934592'::jsonb
 OR proof->'parent_swap_max_bytes' IS DISTINCT FROM '0'::jsonb
 OR proof->'parent_cpu_quota_per_sec_usec' IS DISTINCT FROM '2000000'::jsonb
 OR NOT coalesce(proof->>'parent_cgroup_path' LIKE '/sys/fs/cgroup/%',false)
 OR coalesce(proof->>'parent_cgroup_path','') LIKE '%..%'
 OR NOT coalesce(proof->>'shared_config_sha256' ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(proof->>'host_evidence_sha256' ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(proof->>'source_manifest_sha256' ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(proof->>'operation_evidence_sha256' ~ '^[0-9a-f]{64}$',false)
 OR jsonb_typeof(proof->'os_reserve_bytes') IS DISTINCT FROM 'number'
 OR jsonb_typeof(proof->'total_group_caps_bytes') IS DISTINCT FROM 'number'
 OR jsonb_typeof(proof->'physical_ram_bytes') IS DISTINCT FROM 'number'
 OR (proof->>'os_reserve_bytes')::bigint<3221225472
 OR (proof->>'total_group_caps_bytes')::bigint<>30064771072
 OR (proof->>'physical_ram_bytes')::bigint<(proof->>'total_group_caps_bytes')::bigint+(proof->>'os_reserve_bytes')::bigint THEN
 RAISE EXCEPTION 'SHARED_ARCHIVE_EXTERNAL_REVIEW_MISSING';END IF;
 IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname=input.db_principal
  AND rolcanlogin AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication)
 OR NOT pg_has_role(input.db_principal,'photo_worker','MEMBER')
 OR EXISTS(SELECT FROM pg_roles privileged
  WHERE (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreaterole OR privileged.rolcreatedb OR privileged.rolreplication
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
  RAISE EXCEPTION 'PROCESSING_ACTIVATION_LOGIN_PRIVILEGES_INVALID';
 END IF;

 IF input.report_sha IS NULL OR input.report_sha !~ '^[0-9a-f]{64}$' OR octet_length(input.report_raw) NOT BETWEEN 2 AND 1048576
 OR encode(sha256(convert_to(input.report_raw,'UTF8')),'hex')<>input.report_sha
 OR (input.report_raw::jsonb)->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (input.report_raw::jsonb)->>'backend' IS DISTINCT FROM 'cpu'
 OR (input.report_raw::jsonb)->>'lock_id' IS DISTINCT FROM input.lock_id
 OR (input.report_raw::jsonb)->'models' IS DISTINCT FROM '[]'::jsonb THEN
 RAISE EXCEPTION 'SHARED_ARCHIVE_DOCTOR_INVALID';END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry WHERE operation='archive.build' AND version='1' AND resource_class='archive' AND backends ? 'cpu' AND model_id IS NULL) THEN
 RAISE EXCEPTION 'SHARED_ARCHIVE_OPERATION_INVALID';END IF;
 IF EXISTS(SELECT FROM photo.worker_registrations WHERE worker_id=input.archive_id OR db_principal=input.db_principal)
 OR EXISTS(SELECT FROM photo.worker_registrations WHERE node_id=input.node_id AND id<>cpu.id) THEN
 RAISE EXCEPTION 'SHARED_ARCHIVE_ALREADY_ATTACHED';END IF;
 INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,
 qualified_operations,db_principal,node_id,enabled,certificate_sha256,capacity_policy_sha)
 VALUES(input.tenant_id,input.archive_id,'["archive"]','["cpu"]',input.lock_id,'epoch','{}','{}',input.db_principal,input.node_id,false,input.certificate_sha,cpu.capacity_policy_sha);
END $$;
COMMIT;
-- Existing CPU authority/node budgets/doctor stay unchanged; no extra host budget.
