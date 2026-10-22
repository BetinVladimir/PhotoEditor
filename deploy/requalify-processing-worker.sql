-- Operator-only additive requalification of an existing stopped processing worker.
-- One psql variable processing_requalification_json. See runbook; no migration/grants.
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='30s';
CREATE TEMP TABLE processing_requalification_input ON COMMIT DROP AS
SELECT payload,(payload->'expected_worker'->>'tenant_id')::uuid tenant_id,
 (payload->'expected_worker'->>'worker_id')::uuid worker_id,
 payload->'expected_worker'->>'node_id' node_id,
 (payload->'expected_worker'->>'db_principal')::name db_principal,
 payload->>'resource_class' resource_class,payload->>'doctor_report_jcs' report_raw,
 payload->>'doctor_report_file_sha' report_sha,payload->'new_operations' operations
FROM (SELECT :'processing_requalification_json'::jsonb payload) q;
DO $$
DECLARE input record;registration photo.worker_registrations;node photo.resource_nodes;slots jsonb;proof jsonb;policy photo.capacity_policy_timings;
BEGIN
 SELECT * INTO STRICT input FROM processing_requalification_input;
 IF input.resource_class IS NULL OR input.resource_class NOT IN ('cpu','media','archive')
 OR jsonb_typeof(input.payload->'expected_worker') IS DISTINCT FROM 'object'
 OR jsonb_typeof(input.payload->'expected_node') IS DISTINCT FROM 'object'
 OR jsonb_typeof(input.payload->'expected_slots') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'PROCESSING_REQUALIFICATION_INPUT_INVALID';END IF;
 SELECT * INTO STRICT node FROM photo.resource_nodes WHERE node_id=input.node_id FOR UPDATE;
 SELECT * INTO STRICT registration FROM photo.worker_registrations WHERE tenant_id=input.tenant_id AND worker_id=input.worker_id FOR UPDATE;
 PERFORM id FROM photo.resource_slots WHERE node_id=input.node_id ORDER BY slot_name FOR UPDATE;
 SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY s.slot_name),'[]'::jsonb) INTO slots FROM photo.resource_slots s WHERE s.node_id=input.node_id;
 IF to_jsonb(node) IS DISTINCT FROM input.payload->'expected_node'
 OR to_jsonb(registration) IS DISTINCT FROM input.payload->'expected_worker'
 OR slots IS DISTINCT FROM input.payload->'expected_slots'
 OR node.enabled IS DISTINCT FROM registration.enabled OR node.drain_started_at IS NOT NULL
 OR registration.node_id IS DISTINCT FROM node.node_id OR registration.resource_classes<>jsonb_build_array(input.resource_class) OR registration.backends<>'["cpu"]'::jsonb
 OR registration.db_principal IS DISTINCT FROM input.db_principal
 OR NOT coalesce(registration.certificate_sha256 ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(registration.lock_id ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(node.doctor_report_sha256 ~ '^[0-9a-f]{64}$',false)
 OR registration.qualified_operations='{}'::jsonb OR jsonb_typeof(registration.qualified_operations) IS DISTINCT FROM 'object'
 OR EXISTS(SELECT FROM photo.worker_registrations WHERE node_id=input.node_id AND id<>registration.id)
 OR EXISTS(SELECT FROM photo.resource_slots WHERE node_id=input.node_id AND (task_id IS NOT NULL OR tenant_id IS NOT NULL OR fencing_token IS NOT NULL OR lease_until IS NOT NULL))
 OR EXISTS(SELECT FROM photo.resource_reservations WHERE node_id=input.node_id AND state='reserved')
 OR EXISTS(SELECT FROM photo.tasks WHERE (worker_id=registration.worker_id OR dispatch_worker_id=registration.worker_id) AND state IN ('queued','running')) THEN
 RAISE EXCEPTION 'PROCESSING_REQUALIFICATION_CAS_OR_IDLE_CONFLICT';END IF;
 SELECT * INTO STRICT policy FROM photo.capacity_policy_timings WHERE policy_sha=registration.capacity_policy_sha FOR SHARE;
 IF encode(sha256(convert_to(policy.policy_jcs,'UTF8')),'hex')<>registration.capacity_policy_sha
 OR node.ram_budget_bytes IS DISTINCT FROM ((policy.policy_jcs::jsonb)->>(CASE WHEN input.resource_class='cpu' THEN 'control_ram_budget_bytes' ELSE 'photo_ram_budget_bytes' END))::bigint
 OR node.vram_budget_bytes<>0 OR node.scratch_budget_bytes<1
 OR jsonb_array_length(slots)<>(CASE WHEN input.resource_class='cpu' THEN (policy.policy_jcs::jsonb->>'cpu_concurrency')::integer ELSE 1 END)
 OR EXISTS(SELECT FROM photo.resource_slots WHERE node_id=input.node_id AND resource_class<>input.resource_class)
 OR input.report_sha IS DISTINCT FROM node.doctor_report_sha256 THEN
 RAISE EXCEPTION 'PROCESSING_REQUALIFICATION_POLICY_OR_DOCTOR_CONFLICT';END IF;
 proof:=input.payload->'preflight';
 IF jsonb_typeof(proof) IS DISTINCT FROM 'object'
 OR proof->>'status' IS DISTINCT FROM 'OPERATOR_REVIEWED_PROCESSING_ADDITIONS'
 OR (proof->>'reviewed_at')::timestamptz IS NULL
 OR (proof->>'reviewed_at')::timestamptz NOT BETWEEN clock_timestamp()-interval '10 minutes' AND clock_timestamp()
 OR proof->>'worker_id' IS DISTINCT FROM registration.worker_id::text
 OR proof->'worker_stopped' IS DISTINCT FROM 'true'::jsonb OR proof->'scheduler_stopped' IS DISTINCT FROM 'true'::jsonb OR proof->'rabbit_empty' IS DISTINCT FROM 'true'::jsonb
 OR NOT coalesce(proof->>'runtime_lock_sha256' ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(proof->>'host_evidence_sha256' ~ '^[0-9a-f]{64}$',false)
 OR NOT coalesce(proof->>'operation_evidence_sha256' ~ '^[0-9a-f]{64}$',false) THEN
 RAISE EXCEPTION 'PROCESSING_REQUALIFICATION_EXTERNAL_REVIEW_MISSING';END IF;
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
 IF input.report_sha !~ '^[0-9a-f]{64}$' OR octet_length(input.report_raw) NOT BETWEEN 2 AND 1048576
 OR encode(sha256(convert_to(input.report_raw,'UTF8')),'hex')<>input.report_sha
 OR (input.report_raw::jsonb)->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (input.report_raw::jsonb)->>'backend' IS DISTINCT FROM 'cpu'
 OR (input.report_raw::jsonb)->>'lock_id' IS DISTINCT FROM registration.lock_id
 OR jsonb_typeof((input.report_raw::jsonb)->'models') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'PROCESSING_ACTIVATION_DOCTOR_REPORT_INVALID';
 END IF;
 IF EXISTS(SELECT FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
  WHERE jsonb_typeof(model) IS DISTINCT FROM 'object'
   OR jsonb_typeof(model->'model_id') IS DISTINCT FROM 'string'
   OR length(coalesce(model->>'model_id','')) NOT BETWEEN 1 AND 256
   OR NOT coalesce(model->>'status'=ANY(ARRAY['passed','failed','unsupported']),false))
 OR EXISTS(SELECT model->>'model_id' FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
  GROUP BY model->>'model_id' HAVING count(*)<>1) THEN
  RAISE EXCEPTION 'PROCESSING_ACTIVATION_DOCTOR_REPORT_INVALID';
 END IF;
 IF jsonb_typeof(input.operations) IS DISTINCT FROM 'object' THEN
  RAISE EXCEPTION 'PROCESSING_ACTIVATION_OPERATION_INVENTORY_INVALID';
 END IF;
 IF (SELECT count(*) FROM jsonb_each(input.operations)) NOT BETWEEN 1 AND 128
 OR EXISTS(SELECT FROM jsonb_each(input.operations) item
  LEFT JOIN photo.operation_registry op ON op.operation=item.key AND op.version='1'
  WHERE item.value<>'["cpu"]'::jsonb OR op.operation IS NULL
   OR op.resource_class<>input.resource_class OR NOT(op.backends ? 'cpu')
   OR (op.model_id IS NOT NULL AND NOT EXISTS(
    SELECT FROM jsonb_array_elements((input.report_raw::jsonb)->'models') model
    WHERE model->>'model_id'=op.model_id AND model->>'status'='passed'
     AND model->>'lock_backend_status'='passed'))) THEN
  RAISE EXCEPTION 'PROCESSING_ACTIVATION_OPERATION_UNQUALIFIED';
 END IF;
 IF NOT (input.operations @> registration.qualified_operations) OR input.operations=registration.qualified_operations
 OR EXISTS(SELECT FROM jsonb_each(input.operations) item WHERE item.key NOT IN(SELECT key FROM jsonb_each(registration.qualified_operations)) AND item.key='render.compose') THEN
 RAISE EXCEPTION 'PROCESSING_REQUALIFICATION_ADDITIONS_ONLY';END IF;
 -- Transaction-local disable; never requires an out-of-band manual UPDATE.
 UPDATE photo.worker_registrations SET enabled=false WHERE id=registration.id;
 UPDATE photo.resource_nodes SET enabled=false,ram_available_bytes=0,vram_available_bytes=0,scratch_available_bytes=0 WHERE node_id=input.node_id;
 UPDATE photo.worker_registrations SET qualified_operations=input.operations,enabled=true,last_seen='epoch',reported_capacity_policy_sha=NULL,capacity_policy_reported_at=NULL WHERE id=registration.id;
 UPDATE photo.resource_nodes SET enabled=true,reported_at=NULL,ram_available_bytes=0,vram_available_bytes=0,scratch_available_bytes=0 WHERE node_id=input.node_id;
END $$;
COMMIT;
-- No slots, policy, doctor, budgets, drain generation, credentials or model pins reset.
-- Real authenticated heartbeat required before any admission.
