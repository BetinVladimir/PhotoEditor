-- Operator-only, two-phase capacity change of an existing stopped worker.
-- One psql variable capacity_change_json; see runbook for exact schema.
-- No roles, functions, task states, slots, quality allowlists or model pins are created.
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='30s';
CREATE TEMP TABLE capacity_change_input ON COMMIT DROP AS
SELECT payload, payload->>'phase' AS phase,
 (payload->'expected_worker'->>'tenant_id')::uuid tenant_id,
 (payload->'expected_worker'->>'worker_id')::uuid worker_id,
 payload->'expected_worker'->>'node_id' node_id,
 (payload->'expected_worker'->>'db_principal')::name db_principal,
 payload->>'resource_class' resource_class, payload->>'backend' backend,
 payload->>'new_policy_sha' policy_sha,
 (payload->>'new_ram_bytes')::bigint ram,
 (payload->>'new_vram_bytes')::bigint vram,
 (payload->>'new_scratch_bytes')::bigint scratch,
 payload->>'doctor_report_jcs' report_raw,
 payload->>'doctor_report_file_sha' report_sha,
 payload->'expected_worker'->'qualified_operations' operations
FROM (SELECT :'capacity_change_json'::jsonb payload) q;
DO $$
DECLARE input record; registration photo.worker_registrations; node photo.resource_nodes;
 policy photo.capacity_policy_timings; slots jsonb; proof jsonb;
BEGIN
 SELECT * INTO STRICT input FROM capacity_change_input;
 IF input.phase IS NULL OR input.phase NOT IN ('stage','resume')
 OR input.resource_class IS NULL OR input.resource_class NOT IN ('cpu','media','archive','gpu')
 OR input.backend IS NULL OR input.backend<>(CASE WHEN input.resource_class='gpu' THEN 'cuda' ELSE 'cpu' END)
 OR input.policy_sha IS NULL OR input.policy_sha !~ '^[0-9a-f]{64}$'
 OR input.ram IS NULL OR input.ram NOT BETWEEN 1 AND 9007199254740991
 OR input.vram IS NULL OR input.vram NOT BETWEEN 0 AND 9007199254740991
 OR input.scratch IS NULL OR input.scratch NOT BETWEEN 1 AND 9007199254740991
 OR (input.resource_class<>'gpu' AND input.vram<>0)
 OR jsonb_typeof(input.payload->'expected_worker') IS DISTINCT FROM 'object'
 OR jsonb_typeof(input.payload->'expected_node') IS DISTINCT FROM 'object'
 OR jsonb_typeof(input.payload->'expected_slots') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'CAPACITY_CHANGE_INPUT_INVALID';
 END IF;
 -- Locks match resource admission order: node before its slots/registration.
 SELECT * INTO STRICT node FROM photo.resource_nodes WHERE node_id=input.node_id FOR UPDATE;
 SELECT * INTO STRICT registration FROM photo.worker_registrations
 WHERE tenant_id=input.tenant_id AND worker_id=input.worker_id FOR UPDATE;
 PERFORM id FROM photo.resource_slots WHERE node_id=input.node_id ORDER BY slot_name FOR UPDATE;
 SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY s.slot_name),'[]'::jsonb) INTO slots
 FROM photo.resource_slots s WHERE s.node_id=input.node_id;
 IF to_jsonb(node) IS DISTINCT FROM input.payload->'expected_node'
 OR to_jsonb(registration) IS DISTINCT FROM input.payload->'expected_worker'
 OR slots IS DISTINCT FROM input.payload->'expected_slots'
 OR node.enabled OR registration.enabled
 OR node.ram_available_bytes<>0 OR node.vram_available_bytes<>0 OR node.scratch_available_bytes<>0
 OR registration.node_id IS DISTINCT FROM node.node_id
 OR registration.resource_classes<>jsonb_build_array(input.resource_class)
 OR registration.backends<>jsonb_build_array(input.backend)
 OR registration.certificate_sha256 IS NULL OR registration.certificate_sha256 !~ '^[0-9a-f]{64}$'
 OR registration.lock_id IS NULL OR registration.lock_id !~ '^[0-9a-f]{64}$'
 OR registration.capacity_policy_sha IS NULL
 OR jsonb_typeof(registration.qualified_operations) IS DISTINCT FROM 'object'
 OR registration.qualified_operations='{}'::jsonb
 OR EXISTS(SELECT FROM photo.worker_registrations WHERE node_id=node.node_id AND id<>registration.id)
 OR EXISTS(SELECT FROM photo.resource_slots WHERE node_id=node.node_id AND
  (task_id IS NOT NULL OR tenant_id IS NOT NULL OR fencing_token IS NOT NULL OR lease_until IS NOT NULL))
 OR EXISTS(SELECT FROM photo.resource_reservations WHERE node_id=node.node_id AND state='reserved')
 OR EXISTS(SELECT FROM photo.tasks WHERE (worker_id=registration.worker_id OR dispatch_worker_id=registration.worker_id) AND state IN ('queued','running')) THEN
  RAISE EXCEPTION 'CAPACITY_CHANGE_CAS_OR_IDLE_CONFLICT';
 END IF;
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
  RAISE EXCEPTION 'CAPACITY_CHANGE_LOGIN_PRIVILEGES_INVALID';
 END IF;

 SELECT * INTO STRICT policy FROM photo.capacity_policy_timings WHERE policy_sha=input.policy_sha FOR SHARE;
 IF encode(sha256(convert_to(policy.policy_jcs,'UTF8')),'hex')<>input.policy_sha
 OR (policy.policy_jcs::jsonb->>'schema_version')::integer<>1
 OR (policy.policy_jcs::jsonb->>'offline_seconds')::integer<>policy.offline_seconds
 OR (policy.policy_jcs::jsonb->>'lease_seconds')::integer<>policy.lease_seconds
 OR (policy.policy_jcs::jsonb->>(CASE WHEN input.resource_class='cpu' THEN 'control_ram_budget_bytes' ELSE 'photo_ram_budget_bytes' END))::bigint IS DISTINCT FROM input.ram
 OR (input.resource_class='gpu' AND (policy.policy_jcs::jsonb->>'gpu_budget_bytes')::bigint IS DISTINCT FROM input.vram)
 OR jsonb_array_length(slots)<>(CASE WHEN input.resource_class='cpu' THEN (policy.policy_jcs::jsonb->>'cpu_concurrency')::integer ELSE 1 END)
 OR EXISTS(SELECT FROM photo.resource_slots WHERE node_id=node.node_id AND resource_class<>input.resource_class) THEN
  RAISE EXCEPTION 'CAPACITY_CHANGE_POLICY_OR_SLOT_MISMATCH';
 END IF;
 -- An operator attestation binds external evidence. SQL does not measure OS,
 -- Rabbit, process cgroups, runtime or photographic quality.
 proof=input.payload->'preflight';
 IF jsonb_typeof(proof) IS DISTINCT FROM 'object'
 OR proof->>'status' IS DISTINCT FROM 'OPERATOR_REVIEWED_STOPPED_HOST_CAPACITY'
 OR (proof->>'reviewed_at')::timestamptz IS NULL
 OR (proof->>'reviewed_at')::timestamptz NOT BETWEEN clock_timestamp()-interval '10 minutes' AND clock_timestamp()
 OR proof->>'worker_id' IS DISTINCT FROM registration.worker_id::text
 OR proof->>'new_policy_sha' IS DISTINCT FROM input.policy_sha
 OR proof->'scheduler_stopped' IS DISTINCT FROM 'true'::jsonb
 OR proof->'worker_stopped' IS DISTINCT FROM 'true'::jsonb
 OR proof->'rabbit_empty' IS DISTINCT FROM 'true'::jsonb
 OR (proof->>'physical_ram_bytes')::bigint IS NULL
 OR (proof->>'total_worker_caps_bytes')::bigint IS NULL
 OR (proof->>'os_reserve_bytes')::bigint IS NULL
 OR (proof->>'total_worker_caps_bytes')::bigint<input.ram
 OR (proof->>'os_reserve_bytes')::bigint<3221225472
 OR (proof->>'physical_ram_bytes')::bigint-(proof->>'total_worker_caps_bytes')::bigint < (proof->>'os_reserve_bytes')::bigint
 OR NOT coalesce(proof->>'host_evidence_sha256' ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(proof->>'resource_measurement_sha256' ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(proof->>'runtime_lock_sha256' ~ '^[0-9a-f]{64}$',false) THEN
  RAISE EXCEPTION 'CAPACITY_CHANGE_EXTERNAL_PREFLIGHT_INVALID';
 END IF;
 IF input.phase='stage' THEN
  IF node.doctor_report_sha256 IS NULL OR registration.capacity_policy_sha=input.policy_sha THEN
   RAISE EXCEPTION 'CAPACITY_STAGE_REQUIRES_EXISTING_DIFFERENT_POLICY';
  END IF;
  UPDATE photo.resource_nodes SET ram_budget_bytes=input.ram,vram_budget_bytes=input.vram,
   scratch_budget_bytes=input.scratch,doctor_report_sha256=NULL,reported_at=NULL,
   ram_available_bytes=0,vram_available_bytes=0,scratch_available_bytes=0 WHERE node_id=node.node_id;
  UPDATE photo.worker_registrations SET capacity_policy_sha=input.policy_sha,last_seen='epoch',capacity='{}'::jsonb,
   reported_capacity_policy_sha=NULL,capacity_policy_reported_at=NULL WHERE id=registration.id;
 ELSE
  IF node.doctor_report_sha256 IS NOT NULL OR node.reported_at IS NOT NULL
  OR registration.last_seen<>'epoch'::timestamptz OR registration.capacity<>'{}'::jsonb
  OR registration.reported_capacity_policy_sha IS NOT NULL OR registration.capacity_policy_reported_at IS NOT NULL
  OR registration.capacity_policy_sha<>input.policy_sha OR node.ram_budget_bytes<>input.ram
  OR node.vram_budget_bytes<>input.vram OR node.scratch_budget_bytes<>input.scratch
  OR proof->'resource_and_quality_review_passed' IS DISTINCT FROM 'true'::jsonb
  OR NOT coalesce(proof->>'qualification_evidence_sha256' ~ '^[0-9a-f]{64}$',false) THEN
   RAISE EXCEPTION 'CAPACITY_RESUME_STALE_OR_UNREVIEWED';
  END IF;
 IF input.report_sha !~ '^[0-9a-f]{64}$' OR octet_length(input.report_raw) NOT BETWEEN 2 AND 1048576
 OR encode(sha256(convert_to(input.report_raw,'UTF8')),'hex')<>input.report_sha
 OR (input.report_raw::jsonb)->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (input.report_raw::jsonb)->>'backend' IS DISTINCT FROM input.backend
 OR (input.report_raw::jsonb)->>'lock_id' IS DISTINCT FROM registration.lock_id
 OR jsonb_typeof((input.report_raw::jsonb)->'models') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'CAPACITY_RESUME_DOCTOR_REPORT_INVALID';
 END IF;
 IF EXISTS(SELECT FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
  WHERE jsonb_typeof(model) IS DISTINCT FROM 'object'
   OR jsonb_typeof(model->'model_id') IS DISTINCT FROM 'string'
   OR length(coalesce(model->>'model_id','')) NOT BETWEEN 1 AND 256
   OR NOT coalesce(model->>'status'=ANY(ARRAY['passed','failed','unsupported']),false))
 OR EXISTS(SELECT model->>'model_id' FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
  GROUP BY model->>'model_id' HAVING count(*)<>1) THEN
  RAISE EXCEPTION 'CAPACITY_RESUME_DOCTOR_REPORT_INVALID';
 END IF;
 IF jsonb_typeof(input.operations) IS DISTINCT FROM 'object' THEN
  RAISE EXCEPTION 'CAPACITY_RESUME_OPERATION_INVENTORY_INVALID';
 END IF;
 IF (SELECT count(*) FROM jsonb_each(input.operations)) NOT BETWEEN 1 AND 128
 OR EXISTS(SELECT FROM jsonb_each(input.operations) item
  LEFT JOIN photo.operation_registry op ON op.operation=item.key AND op.version='1'
  WHERE item.value<>jsonb_build_array(input.backend) OR op.operation IS NULL
   OR op.resource_class<>input.resource_class OR NOT(op.backends ? input.backend)
   OR (op.model_id IS NOT NULL AND NOT EXISTS(
    SELECT FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
    WHERE model->>'model_id'=op.model_id AND model->>'status'='passed'
     AND model->>'lock_backend_status'='passed'))) THEN
  RAISE EXCEPTION 'CAPACITY_RESUME_OPERATION_UNQUALIFIED';
 END IF;
  UPDATE photo.resource_nodes SET enabled=true,doctor_report_sha256=input.report_sha,
   drain_started_at=NULL WHERE node_id=node.node_id;
  UPDATE photo.worker_registrations SET enabled=true WHERE id=registration.id;
 END IF;
END $$;
COMMIT;
-- Both phases leave availability stale/zero until a real authenticated heartbeat.
