-- Operator-only psql template. Never run with the worker login.
-- Required variables: tenant_id, worker_id, node_id, db_principal,
-- model_lock_sha, certificate_sha, capacity_policy_sha, ram_budget_bytes, scratch_budget_bytes.
-- Provision the dedicated LOGIN separately; this template contains no passwords.
-- Existing identities are never overwritten. A conflict aborts the transaction.
\set ON_ERROR_STOP on
BEGIN;
CREATE TEMP TABLE rtx_provision_input ON COMMIT DROP AS
SELECT :'tenant_id'::uuid AS tenant_id, :'worker_id'::uuid AS worker_id,
       :'node_id'::text AS node_id, :'db_principal'::name AS db_principal,
       :'model_lock_sha'::text AS model_lock_sha,
       :'certificate_sha'::text AS certificate_sha,
       :'capacity_policy_sha'::text AS capacity_policy_sha,
       :'ram_budget_bytes'::bigint AS ram_budget,
       :'scratch_budget_bytes'::bigint AS scratch_budget;
DO $$
DECLARE input record;
BEGIN
 SELECT * INTO STRICT input FROM rtx_provision_input;
 IF input.tenant_id='00000000-0000-0000-0000-000000000000'::uuid
 OR input.worker_id='00000000-0000-0000-0000-000000000000'::uuid
 OR length(input.node_id) NOT BETWEEN 1 AND 120
 OR input.model_lock_sha !~ '^[0-9a-f]{64}$'
 OR input.certificate_sha !~ '^[0-9a-f]{64}$'
 OR input.capacity_policy_sha !~ '^[0-9a-f]{64}$'
 OR input.ram_budget NOT BETWEEN 1 AND 9007199254740991
 OR input.scratch_budget NOT BETWEEN 1 AND 9007199254740991 THEN
  RAISE EXCEPTION 'INVALID_RTX_PROVISIONING_INPUT';
 END IF;
 -- Bind the already installed canonical policy rather than a caller's timings.
 PERFORM policy_sha FROM photo.capacity_policy_timings
 WHERE policy_sha=input.capacity_policy_sha
 AND (policy_jcs::jsonb)->'gpu_budget_bytes'='10000000000'::jsonb
 AND (policy_jcs::jsonb)->'gpu_concurrency'='1'::jsonb
 AND ((policy_jcs::jsonb)->>'photo_ram_budget_bytes')::bigint=input.ram_budget
 FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'RTX_CAPACITY_POLICY_NOT_INSTALLED_OR_MISMATCHED';
 END IF;
 IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname=input.db_principal
                AND rolcanlogin AND NOT rolsuper AND NOT rolbypassrls
                AND NOT rolcreaterole AND NOT rolcreatedb AND NOT rolreplication)
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
  RAISE EXCEPTION 'RTX_WORKER_LOGIN_NOT_PROVISIONED';
 END IF;
 IF EXISTS (SELECT FROM photo.worker_registrations
            WHERE worker_id=input.worker_id OR db_principal=input.db_principal) THEN
  RAISE EXCEPTION 'RTX_WORKER_IDENTITY_ALREADY_REGISTERED';
 END IF;
END $$;
INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,
 vram_budget_bytes,scratch_budget_bytes)
SELECT node_id,false,ram_budget,10000000000,scratch_budget FROM rtx_provision_input;
INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,
 backends,lock_id,last_seen,capacity,qualified_operations,db_principal,node_id,
 enabled,certificate_sha256,capacity_policy_sha)
SELECT tenant_id,worker_id,'["gpu"]'::jsonb,'["cuda"]'::jsonb,model_lock_sha,
 clock_timestamp(),'{}'::jsonb,'{}'::jsonb,db_principal,node_id,false,certificate_sha,capacity_policy_sha
FROM rtx_provision_input;
COMMIT;
-- No doctor report, qualified operation, resource slot or enabled flag is
-- fabricated. Import actual qualification and activate through operator tooling.
