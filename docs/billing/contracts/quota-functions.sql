-- Internal quota kernel only. No API/worker/maintenance EXECUTE grants at this stage.
-- Tenant lifecycle -> per-resource balance mutex -> buckets -> leases/claims.
-- No network effects. All mutations and journal entries share the caller's transaction.
CREATE FUNCTION photo.billing_quota_lock(p_tenant uuid,p_resource text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_TENANT_INACTIVE' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM photo.billing_accounting_controls WHERE tenant_id=p_tenant AND mode='shadow' AND lifecycle='active' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_ACCOUNTING_DISABLED' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM photo.billing_resource_types WHERE code=p_resource AND kind='consumable' AND value_type='integer';
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_RESOURCE_INVALID' USING ERRCODE='P0001'; END IF;
 INSERT INTO photo.billing_resource_balances(tenant_id,resource_code) VALUES(p_tenant,p_resource) ON CONFLICT DO NOTHING;
 PERFORM 1 FROM photo.billing_resource_balances WHERE tenant_id=p_tenant AND resource_code=p_resource FOR UPDATE;
END $$;

CREATE FUNCTION photo.billing_append_quota_event(p_tenant uuid,p_resource text,p_bucket uuid,p_lease uuid,p_period uuid,p_claim uuid,p_event text,p_quantity bigint,p_direction text,p_key text) RETURNS void
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 INSERT INTO photo.billing_resource_ledger(tenant_id,resource_code,bucket_id,lease_id,period_id,claim_id,event_type,quantity,direction,business_key)
 VALUES(p_tenant,p_resource,p_bucket,p_lease,p_period,p_claim,p_event,p_quantity,p_direction,p_key);
$$;

CREATE FUNCTION photo.billing_grant_bootstrap(p_tenant uuid,p_grant uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE g photo.billing_bootstrap_grants%ROWTYPE; b uuid; cover bigint;
BEGIN
 SELECT * INTO g FROM photo.billing_bootstrap_grants WHERE tenant_id=p_tenant AND id=p_grant;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_GRANT_INVALID'; END IF;
 PERFORM photo.billing_quota_lock(p_tenant,g.resource_code);
 SELECT id INTO b FROM photo.billing_credit_buckets WHERE tenant_id=p_tenant AND bootstrap_grant_id=p_grant;
 IF FOUND THEN RETURN b; END IF;
 IF g.starts_at>clock_timestamp() OR (g.expires_at IS NOT NULL AND g.expires_at<=clock_timestamp()) THEN RAISE EXCEPTION 'BILLING_GRANT_NOT_CURRENT'; END IF;
 INSERT INTO photo.billing_credit_buckets(tenant_id,resource_code,bootstrap_grant_id,original_quantity,remaining_quantity,starts_at,expires_at,expiry_policy,source_type)
 VALUES(p_tenant,g.resource_code,g.id,g.quantity,g.quantity,g.starts_at,g.expires_at,g.expiry_policy,CASE WHEN g.reason='trial_once' THEN 'trial' ELSE 'beta_transition' END) RETURNING id INTO b;
 PERFORM photo.billing_append_quota_event(p_tenant,g.resource_code,b,NULL,NULL,NULL,'grant',g.quantity,'credit','grant/'||g.id);
 SELECT least(overrun_quantity,g.quantity) INTO cover FROM photo.billing_resource_balances WHERE tenant_id=p_tenant AND resource_code=g.resource_code;
 IF cover>0 THEN
  UPDATE photo.billing_credit_buckets SET remaining_quantity=remaining_quantity-cover,consumed_quantity=consumed_quantity+cover WHERE id=b AND tenant_id=p_tenant;
  UPDATE photo.billing_resource_balances SET overrun_quantity=overrun_quantity-cover WHERE tenant_id=p_tenant AND resource_code=g.resource_code;
  PERFORM photo.billing_append_quota_event(p_tenant,g.resource_code,b,NULL,NULL,NULL,'overrun_cover',cover,'debit','cover/'||g.id);
 END IF;
 UPDATE photo.billing_resource_balances SET revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=g.resource_code;
 RETURN b;
END $$;

CREATE FUNCTION photo.billing_allocate_admission_lease(p_tenant uuid,p_resource text,p_period uuid,p_instance text,p_generation bigint,p_policy_version integer,p_key text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pol photo.billing_admission_policies%ROWTYPE; per photo.billing_periods%ROWTYPE; budget photo.billing_allowance_budgets%ROWTYPE;
 old photo.billing_admission_leases%ROWTYPE; bucket photo.billing_credit_buckets%ROWTYPE;
 n timestamptz; deadline timestamptz; available numeric; paid bigint; extra bigint; needed bigint; q bigint; lid uuid; aid uuid;
BEGIN
 IF p_instance IS NULL OR length(p_instance) NOT BETWEEN 1 AND 120 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_generation IS NULL OR p_generation<=0 OR p_policy_version IS NULL THEN RAISE EXCEPTION 'BILLING_LEASE_INPUT_INVALID'; END IF;
 PERFORM photo.billing_quota_lock(p_tenant,p_resource);
 SELECT * INTO old FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=p_period AND request_key=p_key;
 IF FOUND THEN
  IF old.instance_id<>p_instance OR old.generation<>p_generation OR old.policy_version<>p_policy_version THEN RAISE EXCEPTION 'BILLING_IDEMPOTENCY_CONFLICT'; END IF;
  RETURN old.id;
 END IF;
 n:=clock_timestamp();
 SELECT * INTO per FROM photo.billing_periods WHERE tenant_id=p_tenant AND id=p_period AND state='open' AND starts_at<=n AND ends_at>n;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_PERIOD_INVALID'; END IF;
 SELECT * INTO pol FROM photo.billing_admission_policies WHERE resource_code=p_resource AND policy_version=p_policy_version;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_POLICY_INVALID'; END IF;
 -- Disabled catalog policies may only be exercised in explicitly opted-in shadow controls.
 IF EXISTS(SELECT 1 FROM photo.billing_resource_balances WHERE tenant_id=p_tenant AND resource_code=p_resource AND overrun_quantity>0) THEN RAISE EXCEPTION 'BILLING_OVERRUN_BLOCKED'; END IF;
 IF (SELECT count(*) FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=p_resource AND instance_id=p_instance AND state='active')>=pol.max_local_leases THEN RAISE EXCEPTION 'BILLING_LOCAL_LEASE_LIMIT'; END IF;
 IF EXISTS(SELECT 1 FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=p_period AND instance_id=p_instance AND generation>=p_generation) THEN RAISE EXCEPTION 'BILLING_STALE_GENERATION'; END IF;
 INSERT INTO photo.billing_allowance_budgets(tenant_id,resource_code,period_id,configured_quantity,policy_version)
 VALUES(p_tenant,p_resource,p_period,pol.optimistic_allowance,p_policy_version) ON CONFLICT DO NOTHING;
 SELECT * INTO budget FROM photo.billing_allowance_budgets WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=p_period FOR UPDATE;
 IF budget.policy_version<>p_policy_version OR budget.configured_quantity<>pol.optimistic_allowance THEN RAISE EXCEPTION 'BILLING_PERIOD_POLICY_PINNED'; END IF;
 SELECT coalesce(sum(remaining_quantity),0) INTO available FROM photo.billing_credit_buckets WHERE tenant_id=p_tenant AND resource_code=p_resource AND state='active' AND starts_at<=n AND (expires_at IS NULL OR expires_at>n);
 paid:=least(pol.chunk_quantity::numeric,available)::bigint;
 extra:=least(pol.chunk_quantity-paid,budget.configured_quantity-budget.outstanding_quantity-budget.settled_quantity);
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

-- A funding claim is escrow, not proof of an authorized user run or canonical result.
-- The later atomic admission wrapper will attach it to a billing_usage_ticket.
CREATE FUNCTION photo.billing_issue_funding_claim(p_tenant uuid,p_resource text,p_lease uuid,p_fence uuid,p_key text,p_hash bytea,p_quantity bigint) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE l photo.billing_admission_leases%ROWTYPE; old photo.billing_funding_claims%ROWTYPE; a photo.billing_lease_allocations%ROWTYPE; cid uuid; needed bigint; q bigint;
BEGIN
 IF p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_hash IS NULL OR octet_length(p_hash)<>32 OR p_quantity IS NULL OR p_quantity<=0 THEN RAISE EXCEPTION 'BILLING_CLAIM_INPUT_INVALID'; END IF;
 PERFORM photo.billing_quota_lock(p_tenant,p_resource);
 SELECT * INTO l FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=p_resource AND id=p_lease FOR UPDATE;
 IF NOT FOUND OR l.fence_token IS DISTINCT FROM p_fence THEN RAISE EXCEPTION 'BILLING_FENCE_INVALID'; END IF;
 SELECT * INTO old FROM photo.billing_funding_claims WHERE tenant_id=p_tenant AND resource_code=p_resource AND request_key=p_key;
 IF FOUND THEN
  IF old.lease_id<>p_lease OR old.quantity<>p_quantity OR old.request_hash IS DISTINCT FROM p_hash THEN RAISE EXCEPTION 'BILLING_IDEMPOTENCY_CONFLICT'; END IF;
  RETURN old.id;
 END IF;
 IF l.state<>'active' OR l.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'BILLING_LEASE_EXPIRED'; END IF;
 IF EXISTS(SELECT 1 FROM photo.billing_resource_balances WHERE tenant_id=p_tenant AND resource_code=p_resource AND overrun_quantity>0) THEN RAISE EXCEPTION 'BILLING_OVERRUN_BLOCKED'; END IF;
 IF l.allocated_quantity::numeric+l.allowance_quantity-l.issued_quantity-l.returned_quantity<p_quantity THEN RAISE EXCEPTION 'BILLING_QUOTA_EXCEEDED'; END IF;
 INSERT INTO photo.billing_funding_claims(tenant_id,resource_code,period_id,lease_id,request_key,request_hash,quantity) VALUES(p_tenant,p_resource,l.period_id,p_lease,p_key,p_hash,p_quantity) RETURNING id INTO cid;
 needed:=p_quantity;
 FOR a IN SELECT la.* FROM photo.billing_lease_allocations la LEFT JOIN photo.billing_credit_buckets b ON b.tenant_id=la.tenant_id AND b.id=la.bucket_id WHERE la.tenant_id=p_tenant AND la.lease_id=p_lease AND la.quantity>la.issued_quantity+la.returned_quantity ORDER BY (la.source='allowance'),b.expires_at NULLS LAST,la.id FOR UPDATE OF la LOOP
  EXIT WHEN needed=0;
  q:=least(needed,a.quantity-a.issued_quantity-a.returned_quantity);
  INSERT INTO photo.billing_funding_claim_allocations(tenant_id,resource_code,period_id,lease_id,claim_id,lease_allocation_id,quantity) VALUES(p_tenant,p_resource,l.period_id,p_lease,cid,a.id,q);
  UPDATE photo.billing_lease_allocations SET issued_quantity=issued_quantity+q WHERE tenant_id=p_tenant AND id=a.id;
  PERFORM photo.billing_append_quota_event(p_tenant,p_resource,a.bucket_id,p_lease,l.period_id,cid,'issue',q,'transfer','issue/'||cid||'/'||a.id);
  needed:=needed-q;
 END LOOP;
 IF needed<>0 THEN RAISE EXCEPTION 'BILLING_ALLOCATION_INCONSISTENT'; END IF;
 UPDATE photo.billing_admission_leases SET issued_quantity=issued_quantity+p_quantity WHERE tenant_id=p_tenant AND id=p_lease;
 UPDATE photo.billing_resource_balances SET revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=p_resource;
 RETURN cid;
END $$;

-- Only the future trusted atomic adoption/terminal wrapper may call this primitive.
-- p_proof is a deduplication digest; it is NOT a verifier of the external result.
CREATE FUNCTION photo.billing_finalize_funding_claim(p_tenant uuid,p_resource text,p_claim uuid,p_confirmed bigint,p_proof bytea) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c photo.billing_funding_claims%ROWTYPE; r record; used bigint; freed bigint; left_used bigint; n timestamptz;
BEGIN
 IF p_confirmed IS NULL OR p_confirmed<0 OR p_proof IS NULL OR octet_length(p_proof)<>32 THEN RAISE EXCEPTION 'BILLING_TERMINAL_INPUT_INVALID'; END IF;
 PERFORM photo.billing_quota_lock(p_tenant,p_resource);
 SELECT * INTO c FROM photo.billing_funding_claims WHERE tenant_id=p_tenant AND resource_code=p_resource AND id=p_claim FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_CLAIM_INVALID'; END IF;
 IF p_confirmed>c.quantity OR (p_resource<>'outgoing_bytes' AND p_confirmed NOT IN (0,c.quantity)) THEN RAISE EXCEPTION 'BILLING_TERMINAL_QUANTITY_INVALID'; END IF;
 IF c.state<>'accepted' THEN
  IF c.confirmed_quantity IS DISTINCT FROM p_confirmed OR c.terminal_proof_hash IS DISTINCT FROM p_proof THEN RAISE EXCEPTION 'BILLING_TERMINAL_CONFLICT'; END IF;
  RETURN c.id;
 END IF;
 left_used:=p_confirmed; n:=clock_timestamp();
 FOR r IN SELECT ca.id AS ca_id,ca.quantity,a.id AS allocation_id,a.source,a.bucket_id,b.expires_at,b.state AS bucket_state FROM photo.billing_funding_claim_allocations ca JOIN photo.billing_lease_allocations a ON a.tenant_id=ca.tenant_id AND a.id=ca.lease_allocation_id LEFT JOIN photo.billing_credit_buckets b ON b.tenant_id=a.tenant_id AND b.id=a.bucket_id WHERE ca.tenant_id=p_tenant AND ca.claim_id=c.id ORDER BY (a.source='allowance'),b.expires_at NULLS LAST,a.id LOOP
  used:=least(left_used,r.quantity); freed:=r.quantity-used;
  IF r.source='paid' THEN
   UPDATE photo.billing_credit_buckets SET leased_quantity=leased_quantity-r.quantity,consumed_quantity=consumed_quantity+used,remaining_quantity=remaining_quantity+CASE WHEN expires_at IS NULL OR expires_at>n THEN freed ELSE 0 END,revoked_quantity=revoked_quantity+CASE WHEN expires_at<=n THEN freed ELSE 0 END WHERE tenant_id=p_tenant AND id=r.bucket_id;
  ELSE
   UPDATE photo.billing_allowance_budgets SET outstanding_quantity=outstanding_quantity-r.quantity,settled_quantity=settled_quantity+used,revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=c.period_id;
   IF used>0 THEN
    UPDATE photo.billing_resource_balances SET overrun_quantity=overrun_quantity+used WHERE tenant_id=p_tenant AND resource_code=p_resource;
    PERFORM photo.billing_append_quota_event(p_tenant,p_resource,NULL,c.lease_id,c.period_id,c.id,'overrun',used,'debit','overrun/'||c.id||'/'||r.ca_id);
   END IF;
  END IF;
  IF used>0 THEN PERFORM photo.billing_append_quota_event(p_tenant,p_resource,r.bucket_id,c.lease_id,c.period_id,c.id,'settle',used,'debit','settle/'||c.id||'/'||r.ca_id); END IF;
  IF freed>0 THEN
   PERFORM photo.billing_append_quota_event(p_tenant,p_resource,r.bucket_id,c.lease_id,c.period_id,c.id,CASE WHEN r.source='paid' AND r.expires_at<=n THEN 'expire' ELSE 'release' END,freed,CASE WHEN r.source='paid' AND r.expires_at<=n THEN 'debit' ELSE 'transfer' END,'release/'||c.id||'/'||r.ca_id);
  END IF;
  left_used:=left_used-used;
 END LOOP;
 IF left_used<>0 OR (SELECT coalesce(sum(quantity),0) FROM photo.billing_funding_claim_allocations WHERE tenant_id=p_tenant AND claim_id=c.id)<>c.quantity THEN RAISE EXCEPTION 'BILLING_ALLOCATION_INCONSISTENT'; END IF;
 UPDATE photo.billing_funding_claims SET state=CASE WHEN p_confirmed=0 THEN 'released' ELSE 'settled' END,confirmed_quantity=p_confirmed,terminal_proof_hash=p_proof WHERE tenant_id=p_tenant AND id=c.id;
 UPDATE photo.billing_admission_leases SET state='closed' WHERE tenant_id=p_tenant AND id=c.lease_id AND state='draining' AND NOT EXISTS(SELECT 1 FROM photo.billing_funding_claims WHERE tenant_id=p_tenant AND lease_id=c.lease_id AND state='accepted');
 UPDATE photo.billing_resource_balances SET revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=p_resource;
 RETURN c.id;
END $$;

CREATE FUNCTION photo.billing_close_admission_lease(p_tenant uuid,p_resource text,p_lease uuid,p_fence uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE l photo.billing_admission_leases%ROWTYPE; a photo.billing_lease_allocations%ROWTYPE; q bigint; total bigint:=0; n timestamptz; expiry timestamptz;
BEGIN
 PERFORM photo.billing_quota_lock(p_tenant,p_resource);
 SELECT * INTO l FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code=p_resource AND id=p_lease FOR UPDATE;
 IF NOT FOUND OR l.fence_token IS DISTINCT FROM p_fence THEN RAISE EXCEPTION 'BILLING_FENCE_INVALID'; END IF;
 IF l.state='closed' THEN RETURN l.id; END IF;
 IF (SELECT coalesce(sum(quantity),0) FROM photo.billing_funding_claims WHERE tenant_id=p_tenant AND lease_id=p_lease)<>l.issued_quantity THEN RAISE EXCEPTION 'BILLING_ISSUANCE_INCONSISTENT'; END IF;
 n:=clock_timestamp();
 FOR a IN SELECT * FROM photo.billing_lease_allocations WHERE tenant_id=p_tenant AND lease_id=p_lease ORDER BY id FOR UPDATE LOOP
  IF a.issued_quantity<>(SELECT coalesce(sum(quantity),0) FROM photo.billing_funding_claim_allocations WHERE tenant_id=p_tenant AND lease_allocation_id=a.id) THEN RAISE EXCEPTION 'BILLING_ISSUANCE_INCONSISTENT'; END IF;
  q:=a.quantity-a.issued_quantity-a.returned_quantity;
  CONTINUE WHEN q=0;
  expiry:=NULL;
  IF a.source='paid' THEN
   SELECT expires_at INTO expiry FROM photo.billing_credit_buckets WHERE tenant_id=p_tenant AND id=a.bucket_id;
   UPDATE photo.billing_credit_buckets SET leased_quantity=leased_quantity-q,remaining_quantity=remaining_quantity+CASE WHEN expires_at IS NULL OR expires_at>n THEN q ELSE 0 END,revoked_quantity=revoked_quantity+CASE WHEN expires_at<=n THEN q ELSE 0 END WHERE tenant_id=p_tenant AND id=a.bucket_id;
  ELSE
   UPDATE photo.billing_allowance_budgets SET outstanding_quantity=outstanding_quantity-q,revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=p_resource AND period_id=l.period_id;
  END IF;
  UPDATE photo.billing_lease_allocations SET returned_quantity=returned_quantity+q WHERE tenant_id=p_tenant AND id=a.id;
  PERFORM photo.billing_append_quota_event(p_tenant,p_resource,a.bucket_id,p_lease,l.period_id,NULL,CASE WHEN expiry<=n THEN 'expire' ELSE 'release' END,q,CASE WHEN expiry<=n THEN 'debit' ELSE 'transfer' END,'close/'||a.id);
  total:=total+q;
 END LOOP;
 UPDATE photo.billing_admission_leases SET returned_quantity=returned_quantity+total,state=CASE WHEN EXISTS(SELECT 1 FROM photo.billing_funding_claims WHERE tenant_id=p_tenant AND lease_id=p_lease AND state='accepted') THEN 'draining' ELSE 'closed' END WHERE tenant_id=p_tenant AND id=p_lease;
 UPDATE photo.billing_resource_balances SET revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=p_resource;
 RETURN p_lease;
END $$;

CREATE FUNCTION photo.billing_expire_quota_buckets(p_tenant uuid,p_resource text,p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.billing_credit_buckets%ROWTYPE; affected integer:=0; n timestamptz;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION 'BILLING_BATCH_INVALID'; END IF;
 PERFORM photo.billing_quota_lock(p_tenant,p_resource); n:=clock_timestamp();
 FOR b IN SELECT * FROM photo.billing_credit_buckets WHERE tenant_id=p_tenant AND resource_code=p_resource AND expires_at<=n AND remaining_quantity>0 ORDER BY expires_at,id LIMIT p_limit FOR UPDATE LOOP
  UPDATE photo.billing_credit_buckets SET remaining_quantity=0,revoked_quantity=revoked_quantity+b.remaining_quantity,state=CASE WHEN leased_quantity>0 THEN 'draining' ELSE 'expired' END WHERE tenant_id=p_tenant AND id=b.id;
  PERFORM photo.billing_append_quota_event(p_tenant,p_resource,b.id,NULL,NULL,NULL,'expire',b.remaining_quantity,'debit','expire/'||b.id||'/'||b.revoked_quantity);
  affected:=affected+1;
 END LOOP;
 IF affected>0 THEN UPDATE photo.billing_resource_balances SET revision=revision+1 WHERE tenant_id=p_tenant AND resource_code=p_resource; END IF;
 RETURN affected;
END $$;

CREATE FUNCTION photo.billing_project_quota(p_tenant uuid,p_resource text) RETURNS TABLE(available numeric,allocated numeric,consumed numeric,revoked numeric,overrun bigint,revision bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT greatest(coalesce(sum(b.remaining_quantity) FILTER(WHERE b.state='active' AND b.starts_at<=statement_timestamp() AND (b.expires_at IS NULL OR b.expires_at>statement_timestamp())),0)-s.overrun_quantity,0),coalesce(sum(b.leased_quantity),0),coalesce(sum(b.consumed_quantity),0)+s.overrun_quantity,coalesce(sum(b.revoked_quantity),0),s.overrun_quantity,s.revision
 FROM photo.billing_resource_balances s JOIN photo.billing_accounting_controls c ON c.tenant_id=s.tenant_id AND c.lifecycle='active' JOIN photo.tenants t ON t.id=s.tenant_id AND t.status='active'
 LEFT JOIN photo.billing_credit_buckets b ON b.tenant_id=s.tenant_id AND b.resource_code=s.resource_code
 WHERE s.tenant_id=p_tenant AND s.resource_code=p_resource GROUP BY s.overrun_quantity,s.revision;
$$;

CREATE FUNCTION photo.billing_quota_reject_immutable() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN RAISE EXCEPTION 'BILLING_IMMUTABLE' USING ERRCODE='23514'; END $$;

CREATE FUNCTION photo.billing_quota_pin_budget() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.settled_quantity<OLD.settled_quantity THEN RAISE EXCEPTION 'BILLING_SETTLED_ALLOWANCE_IMMUTABLE' USING ERRCODE='23514'; END IF;
 IF (NEW.tenant_id,NEW.resource_code,NEW.period_id,NEW.configured_quantity,NEW.policy_version) IS DISTINCT FROM (OLD.tenant_id,OLD.resource_code,OLD.period_id,OLD.configured_quantity,OLD.policy_version) THEN RAISE EXCEPTION 'BILLING_BUDGET_IMMUTABLE' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
