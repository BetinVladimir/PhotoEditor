-- +goose Up
-- Forward-only paid operation funding; normal seven-argument chunk allocator is unchanged.
DO $$
DECLARE old pg_catalog.pg_proc%ROWTYPE;caller pg_catalog.text;
BEGIN
 IF EXISTS(SELECT FROM pg_catalog.pg_roles WHERE rolname='photo_fn_owner'AND rolcanlogin) THEN RAISE EXCEPTION 'BILLING_PAID_OPERATION_OWNER_DRIFT';END IF;
 IF pg_catalog.to_regprocedure('photo.billing_allocate_paid_operation_lease(uuid,text,uuid,text,bigint,integer,text,bigint)')IS NOT NULL THEN RAISE EXCEPTION 'BILLING_PAID_OPERATION_HELPER_OCCUPIED';END IF;
 SELECT * INTO old FROM pg_catalog.pg_proc WHERE oid=pg_catalog.to_regprocedure('photo.billing_bind_operations(uuid,uuid)');
 IF NOT FOUND OR old.proowner<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname='photo_fn_owner') OR old.prolang<>(SELECT oid FROM pg_catalog.pg_language WHERE lanname='plpgsql') OR old.prorettype<>'pg_catalog.uuid'::pg_catalog.regtype OR ARRAY(SELECT unnest(old.proargtypes)) IS DISTINCT FROM ARRAY['pg_catalog.uuid'::pg_catalog.regtype::pg_catalog.oid,'pg_catalog.uuid'::pg_catalog.regtype::pg_catalog.oid] OR old.proargnames IS DISTINCT FROM ARRAY['p_tenant','p_run']::pg_catalog.text[] OR old.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, photo']::pg_catalog.text[] OR old.proacl IS DISTINCT FROM ARRAY['photo_fn_owner=X/photo_fn_owner']::pg_catalog.aclitem[] OR encode(sha256(convert_to(old.prosrc,'UTF8')),'hex')<>'6266b7218059acaab5b218378b249e9a6e45f150f91ea485850e0241c612af9e' OR old.proallargtypes IS NOT NULL OR old.proargdefaults IS NOT NULL OR old.proargmodes IS NOT NULL OR old.probin IS NOT NULL OR old.prosqlbody IS NOT NULL OR old.protrftypes IS NOT NULL OR old.pronargs IS DISTINCT FROM 2 OR old.pronargdefaults IS DISTINCT FROM 0 OR old.procost IS DISTINCT FROM 100 OR old.prorows IS DISTINCT FROM 0 OR old.provariadic IS DISTINCT FROM 0::pg_catalog.oid OR old.prosupport::pg_catalog.oid IS DISTINCT FROM 0::pg_catalog.oid OR old.provolatile IS DISTINCT FROM 'v' OR old.proparallel IS DISTINCT FROM 'u' OR old.prokind IS DISTINCT FROM 'f' OR old.prosecdef IS DISTINCT FROM true OR old.proisstrict IS DISTINCT FROM false OR old.proleakproof IS DISTINCT FROM false OR old.proretset IS DISTINCT FROM false THEN RAISE EXCEPTION 'BILLING_PAID_OPERATION_PREDECESSOR_DRIFT';END IF;
 FOREACH caller IN ARRAY ARRAY['photo_api','photo_scheduler','photo_worker','photo_auth','photo_portal','photo_maintenance']LOOP
  IF pg_catalog.has_function_privilege(caller,old.oid,'EXECUTE')THEN RAISE EXCEPTION 'BILLING_PAID_OPERATION_CALLER_DRIFT';END IF;
 END LOOP;
END $$;

CREATE FUNCTION photo.billing_allocate_paid_operation_lease(p_tenant uuid,p_resource text,p_period uuid,p_instance text,p_generation bigint,p_policy_version integer,p_key text,p_quantity bigint) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pol photo.billing_admission_policies%ROWTYPE; per photo.billing_periods%ROWTYPE; budget photo.billing_allowance_budgets%ROWTYPE;
 old photo.billing_admission_leases%ROWTYPE; bucket photo.billing_credit_buckets%ROWTYPE;
 n timestamptz; deadline timestamptz; available numeric; paid bigint; extra bigint; needed bigint; q bigint; lid uuid; aid uuid;
BEGIN
 IF p_quantity IS NULL OR p_quantity<=0 THEN RAISE EXCEPTION 'BILLING_PAID_OPERATION_QUANTITY_INVALID';END IF;
 IF p_instance IS NULL OR length(p_instance) NOT BETWEEN 1 AND 120 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_generation IS NULL OR p_generation<=0 OR p_policy_version IS NULL THEN RAISE EXCEPTION 'BILLING_LEASE_INPUT_INVALID'; END IF;
 PERFORM photo.billing_quota_lock(p_tenant,p_resource);
 -- No caller-supplied inflation: fund only the already-pinned immutable item.
 PERFORM i.id FROM photo.billing_operation_items i JOIN photo.billing_operation_requests req ON req.tenant_id=i.tenant_id AND req.id=i.request_id
 JOIN photo.billing_operation_policies op ON op.id=i.policy_id AND op.billing_class='paid_operation' AND op.resource_code=i.resource_code AND op.policy_snapshot_hash=i.policy_hash
 WHERE i.tenant_id=p_tenant AND i.resource_code=p_resource AND i.quantity=p_quantity AND i.state='accepted' AND req.state='accepted'
 AND req.period_id=p_period AND p_instance='operation/'||req.run_id AND p_key='op/'||i.id AND p_resource LIKE 'operation.%' AND p_resource NOT LIKE 'operation.beta.%'
 FOR SHARE OF i,req;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_PAID_OPERATION_SCOPE_INVALID';END IF;
 SELECT * INTO old FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=p_period AND request_key=p_key;
 IF FOUND THEN
  IF old.instance_id<>p_instance OR old.generation<>p_generation OR old.policy_version<>p_policy_version OR old.allocated_quantity<>p_quantity OR old.allowance_quantity<>0 THEN RAISE EXCEPTION 'BILLING_IDEMPOTENCY_CONFLICT'; END IF;
  RETURN old.id;
 END IF;
 n:=clock_timestamp();
 SELECT * INTO per FROM photo.billing_periods WHERE tenant_id=p_tenant AND id=p_period AND state='open' AND starts_at<=n AND ends_at>n;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_PERIOD_INVALID'; END IF;
 SELECT * INTO pol FROM photo.billing_admission_policies WHERE resource_code=p_resource AND policy_version=p_policy_version;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_POLICY_INVALID'; END IF;
 IF p_quantity<=pol.chunk_quantity THEN RAISE EXCEPTION 'BILLING_PAID_OPERATION_NOT_OVERSIZE';END IF;
 -- Disabled catalog policies may only be exercised in explicitly opted-in shadow controls.
 IF EXISTS(SELECT 1 FROM photo.billing_resource_balances WHERE tenant_id=p_tenant AND resource_code=p_resource AND overrun_quantity>0) THEN RAISE EXCEPTION 'BILLING_OVERRUN_BLOCKED'; END IF;
 IF (SELECT count(*) FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=p_resource AND instance_id=p_instance AND state='active')>=pol.max_local_leases THEN RAISE EXCEPTION 'BILLING_LOCAL_LEASE_LIMIT'; END IF;
 IF EXISTS(SELECT 1 FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=p_period AND instance_id=p_instance AND generation>=p_generation) THEN RAISE EXCEPTION 'BILLING_STALE_GENERATION'; END IF;
 INSERT INTO photo.billing_allowance_budgets(tenant_id,resource_code,period_id,configured_quantity,policy_version)
 VALUES(p_tenant,p_resource,p_period,pol.optimistic_allowance,p_policy_version) ON CONFLICT DO NOTHING;
 SELECT * INTO budget FROM photo.billing_allowance_budgets WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=p_period FOR UPDATE;
 IF budget.policy_version<>p_policy_version OR budget.configured_quantity<>pol.optimistic_allowance THEN RAISE EXCEPTION 'BILLING_PERIOD_POLICY_PINNED'; END IF;
 SELECT coalesce(sum(remaining_quantity),0) INTO available FROM photo.billing_credit_buckets WHERE tenant_id=p_tenant AND resource_code=p_resource AND state='active' AND starts_at<=n AND (expires_at IS NULL OR expires_at>n);
 IF available<p_quantity THEN RAISE EXCEPTION 'BILLING_QUOTA_EXCEEDED';END IF;
 -- One exact paid item is not an optimistic/cache chunk; never borrow allowance.
 paid:=p_quantity;
 extra:=0;
 IF paid=0 AND extra=0 THEN RAISE EXCEPTION 'BILLING_QUOTA_EXCEEDED'; END IF;
 deadline:=least(per.ends_at,n+make_interval(secs=>pol.lease_seconds));
 INSERT INTO photo.billing_admission_leases(tenant_id,resource_code,period_id,instance_id,generation,request_key,policy_version,allocated_quantity,allowance_quantity,expires_at)
 VALUES(p_tenant,p_resource,p_period,p_instance,p_generation,p_key,p_policy_version,paid,extra,deadline) RETURNING id INTO lid;
 needed:=paid;
 FOR bucket IN SELECT * FROM photo.billing_credit_buckets WHERE tenant_id=p_tenant AND resource_code=p_resource AND state='active' AND remaining_quantity>0 AND starts_at<=n AND (expires_at IS NULL OR expires_at>n) ORDER BY expires_at NULLS LAST,id FOR UPDATE LOOP
  EXIT WHEN needed=0;
  q:=least(needed,bucket.remaining_quantity);
  UPDATE photo.billing_credit_buckets SET remaining_quantity=remaining_quantity-q,leased_quantity=leased_quantity+q WHERE id=bucket.id AND tenant_id=p_tenant;
  INSERT INTO photo.billing_lease_allocations(tenant_id,resource_code,period_id,lease_id,bucket_id,source,quantity) VALUES(p_tenant,p_resource,p_period,lid,bucket.id,'paid',q) RETURNING id INTO aid;
  PERFORM photo.billing_append_quota_event(p_tenant,p_resource,bucket.id,lid,p_period,NULL,'allocate',q,'transfer','allocate/'||aid);
  IF bucket.expires_at IS NOT NULL THEN deadline:=least(deadline,bucket.expires_at); END IF;
  needed:=needed-q;
 END LOOP;
 IF needed<>0 THEN RAISE EXCEPTION 'BILLING_ALLOCATION_INCONSISTENT'; END IF;
 IF extra>0 THEN
  INSERT INTO photo.billing_lease_allocations(tenant_id,resource_code,period_id,lease_id,source,quantity) VALUES(p_tenant,p_resource,p_period,lid,'allowance',extra) RETURNING id INTO aid;
  UPDATE photo.billing_allowance_budgets SET outstanding_quantity=outstanding_quantity+extra,revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=p_period;
  PERFORM photo.billing_append_quota_event(p_tenant,p_resource,NULL,lid,p_period,NULL,'allocate',extra,'transfer','allocate/'||aid);
 END IF;
 UPDATE photo.billing_admission_leases SET expires_at=deadline WHERE tenant_id=p_tenant AND id=lid;
 UPDATE photo.billing_resource_balances SET revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=p_resource;
 RETURN lid;
END $$;
ALTER FUNCTION photo.billing_allocate_paid_operation_lease(uuid,text,uuid,text,bigint,integer,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_allocate_paid_operation_lease(uuid,text,uuid,text,bigint,integer,text,bigint) FROM PUBLIC,photo_api,photo_scheduler,photo_worker;
REVOKE ALL ON FUNCTION photo.billing_allocate_paid_operation_lease(uuid,text,uuid,text,bigint,integer,text,bigint) FROM photo_auth,photo_portal,photo_maintenance;

CREATE OR REPLACE FUNCTION photo.billing_bind_operations(p_tenant uuid,p_run uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;req photo.billing_operation_requests%ROWTYPE;pol photo.billing_operation_policies%ROWTYPE;
 task record;res record;period uuid;rid uuid;lid uuid;fence uuid;claim uuid;generation bigint;hash bytea;context text;q bigint;available numeric;instance text;source_version uuid;original_sha text;
BEGIN
 IF NOT photo.billing_prepare_operations(p_tenant,p_run) THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT sha256(convert_to(r.config_hash||coalesce(string_agg(t.id::text||'/'||t.operation||'/'||t.operation_version||'/'||t.input_digest,',' ORDER BY t.id),''),'UTF8')) INTO hash FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run;
 SELECT * INTO req FROM photo.billing_operation_requests WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF FOUND THEN
  IF req.request_hash<>hash THEN RAISE EXCEPTION 'BILLING_IDEMPOTENCY_CONFLICT';END IF;
  RETURN req.id;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.billing_operation_children WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN NULL;END IF;
 IF r.graph_sealed OR r.status<>'draft' OR r.cancel_requested THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID';END IF;
 SELECT id INTO period FROM photo.billing_periods WHERE tenant_id=p_tenant AND state='open' AND starts_at<=clock_timestamp() AND ends_at>clock_timestamp();
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_PERIOD_INVALID';END IF;
 SELECT CASE WHEN workflow_type='preview' THEN 'preview' ELSE 'standalone' END INTO context FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 -- Leaf result tasks are the user-visible units; prerequisites are included.
 -- Policy is context-specific: a standard_root policy cannot authorize standalone.
 IF NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run) THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID';END IF;
 rid:=gen_random_uuid();instance:='operation/'||p_run;
 INSERT INTO photo.billing_operation_requests(id,tenant_id,run_id,period_id,request_hash) VALUES(rid,p_tenant,p_run,period,hash);
 FOR task IN SELECT t.* FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges e WHERE e.tenant_id=t.tenant_id AND e.run_id=p_run AND e.prerequisite_id=t.id)
 ORDER BY t.operation,t.operation_version,t.id LOOP
  SELECT * INTO pol FROM photo.billing_operation_policies WHERE operation_code=task.operation AND operation_version=task.operation_version AND charge_context=context AND effective_from<=clock_timestamp() ORDER BY policy_version DESC LIMIT 1;
  IF NOT FOUND OR pol.billing_class NOT IN('paid_operation','free_beta') OR pol.resource_code IS NULL OR pol.resource_code NOT LIKE 'operation.%'
   OR (pol.billing_class='free_beta' AND pol.resource_code NOT LIKE 'operation.beta.%') OR (pol.billing_class='paid_operation' AND pol.resource_code LIKE 'operation.beta.%')
   OR pol.success_contract<>'v2_adopted_outputs_v1' OR pol.rerun_policy<>'none' THEN RAISE EXCEPTION 'BILLING_OPERATION_POLICY_REQUIRED';END IF;
  IF task.node_key !~ ('^'||replace(task.operation,'.','[.]')||':[0-9]{1,4}$') THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID';END IF;
  SELECT v.id,orig.sha256 INTO source_version,original_sha FROM photo.snapshot_items si
  JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id
  JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.deleted_at IS NULL AND a.upload_state='verified'
  JOIN photo.artifacts orig ON orig.tenant_id=a.tenant_id AND orig.id=a.original_artifact_id AND orig.state='ready' AND orig.verified_at IS NOT NULL AND orig.object_version IS NOT NULL AND orig.object_version NOT IN('','null') AND (orig.expires_at IS NULL OR orig.expires_at>clock_timestamp())
  WHERE si.tenant_id=p_tenant AND si.snapshot_id=r.input_snapshot_id AND si.ordinal=split_part(task.node_key,':',2)::integer AND si.artifact_id=v.master_artifact_id AND si.sha256=v.content_hash;
  IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID';END IF;
  q:=CASE WHEN pol.billing_class='free_beta' THEN 1 ELSE pol.units_per_success END;
  IF pol.billing_class='free_beta' AND (SELECT coalesce(sum(i.quantity),0) FROM photo.billing_operation_items i JOIN photo.billing_operation_requests b ON b.tenant_id=i.tenant_id AND b.id=i.request_id WHERE i.tenant_id=p_tenant AND i.policy_id=pol.id AND b.period_id=period AND i.state<>'released')+q>pol.beta_budget THEN RAISE EXCEPTION 'BILLING_BETA_BUDGET_EXCEEDED';END IF;
  INSERT INTO photo.billing_operation_items(tenant_id,request_id,task_id,source_version_id,original_digest,policy_hash,policy_id,resource_code,quantity,claim_id)
  VALUES(p_tenant,rid,task.id,source_version,decode(original_sha,'hex'),pol.policy_snapshot_hash,pol.id,pol.resource_code,q,NULL);
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.billing_operation_items WHERE tenant_id=p_tenant AND request_id=rid) THEN RAISE EXCEPTION 'BILLING_OPERATION_POLICY_REQUIRED';END IF;
 -- Claims are populated after deterministic resource locking and whole-request
 -- deficit checks. Temporary NULL claims must be funded before the deferred scope check.
 FOR res IN SELECT resource_code,sum(quantity)::bigint AS quantity FROM photo.billing_operation_items WHERE tenant_id=p_tenant AND request_id=rid GROUP BY resource_code ORDER BY resource_code LOOP
  PERFORM photo.billing_quota_lock(p_tenant,res.resource_code);
  SELECT coalesce(sum(remaining_quantity),0) INTO available FROM photo.billing_credit_buckets WHERE tenant_id=p_tenant AND resource_code=res.resource_code AND state='active' AND starts_at<=clock_timestamp() AND (expires_at IS NULL OR expires_at>clock_timestamp());
  IF (SELECT sum(quantity) FROM photo.billing_operation_items WHERE tenant_id=p_tenant AND request_id=rid)>1 AND available<res.quantity THEN RAISE EXCEPTION 'BILLING_QUOTA_EXCEEDED';END IF;
 END LOOP;
 FOR task IN SELECT * FROM photo.billing_operation_items WHERE tenant_id=p_tenant AND request_id=rid ORDER BY resource_code,task_id LOOP
  SELECT id,fence_token INTO lid,fence FROM photo.billing_admission_leases
  WHERE tenant_id=p_tenant AND resource_code=task.resource_code AND period_id=period AND instance_id=instance AND state='active' AND expires_at>clock_timestamp()
   AND allocated_quantity+allowance_quantity-issued_quantity-returned_quantity>=task.quantity ORDER BY photo.billing_admission_leases.generation DESC LIMIT 1;
  IF NOT FOUND THEN
   FOR res IN SELECT id,fence_token FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=task.resource_code AND instance_id=instance AND state='active' ORDER BY id LOOP
    PERFORM photo.billing_close_admission_lease(p_tenant,task.resource_code,res.id,res.fence_token);
   END LOOP;
   SELECT coalesce(max(l.generation),0)+1 INTO generation FROM photo.billing_admission_leases l WHERE l.tenant_id=p_tenant AND l.resource_code=task.resource_code AND l.period_id=period AND l.instance_id=instance;
   IF task.quantity>(SELECT chunk_quantity FROM photo.billing_admission_policies WHERE resource_code=task.resource_code AND policy_version=1) THEN
    lid:=photo.billing_allocate_paid_operation_lease(p_tenant,task.resource_code,period,instance,generation,1,'op/'||task.id,task.quantity);
   ELSE
   lid:=photo.billing_allocate_admission_lease(p_tenant,task.resource_code,period,instance,generation,1,'op/'||task.id);
   END IF;
   SELECT fence_token INTO fence FROM photo.billing_admission_leases WHERE id=lid;
  END IF;
  claim:=photo.billing_issue_funding_claim(p_tenant,task.resource_code,lid,fence,'op/'||task.id,hash,task.quantity);
  UPDATE photo.billing_operation_items SET claim_id=claim WHERE tenant_id=p_tenant AND id=task.id;
 END LOOP;
 -- Reuse chunks across billable units in this admission, then return their
 -- unused capacity. No worker step performs another commercial check.
 FOR res IN SELECT id,resource_code,fence_token FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND instance_id=instance AND state='active' ORDER BY resource_code,id LOOP
  PERFORM photo.billing_close_admission_lease(p_tenant,res.resource_code,res.id,res.fence_token);
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,nullif(r.config_snapshot->>'auth_session_id','')::uuid)) THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID';END IF;
 RETURN rid;
END $$;
ALTER FUNCTION photo.billing_bind_operations(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_bind_operations(uuid,uuid) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_auth,photo_portal,photo_maintenance;
