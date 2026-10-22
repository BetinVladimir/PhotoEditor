"""Quota escrow/journal invariants in isolated PostgreSQL, including real multi-session races."""
from concurrent.futures import ThreadPoolExecutor
from hashlib import sha256
from pathlib import Path
from threading import Barrier
from uuid import uuid4
import psycopg
import pytest

ROOT=Path(__file__).resolve().parents[2]
NEW=['02598_billing_catalog_scaffold.sql','02599_billing_catalog_seed.sql','02600_billing_quota_accounting.sql','02601_billing_quota_functions.sql']
pytestmark=pytest.mark.migration_head(17)

def digest(s): return sha256(s.encode()).digest()

def tenant(c):
    u=c.execute("INSERT INTO photo.users(email_normalized) VALUES (%s) RETURNING id",(str(uuid4())+'@example.invalid',)).fetchone()[0]
    with c.transaction():
        t=c.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES ('quota test',%s,'{}','{}') RETURNING id",(u,)).fetchone()[0]
        c.execute("INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES (%s,%s,'owner','active')",(t,u))
    return t

@pytest.fixture
def env(database):
    with psycopg.connect(**database,autocommit=True) as c:
        for name in NEW:
            with c.transaction():c.execute((ROOT/'backend/db/migrations'/name).read_text().split('-- +goose Down')[0])
        t=tenant(c)
        c.execute("INSERT INTO photo.billing_accounting_controls(tenant_id) VALUES (%s)",(t,))
        period=c.execute("INSERT INTO photo.billing_periods(tenant_id,starts_at,ends_at,anchor_kind) VALUES (%s,now()-interval '1 hour',now()+interval '1 hour','trial_calendar') RETURNING id",(t,)).fetchone()[0]
        yield c,t,period,database

def grant(c,t,q=3,resource='standard_photo',trial=False):
    g=c.execute("INSERT INTO photo.billing_bootstrap_grants(tenant_id,resource_code,reason,quantity,starts_at,expires_at,expiry_policy,policy_snapshot_hash) VALUES (%s,%s,%s,%s,now()-interval '1 hour',CASE WHEN %s THEN NULL ELSE now()+interval '1 day' END,%s,%s) RETURNING id",(t,resource,'trial_once' if trial else 'beta_transition',q,trial,'never' if trial else 'fixed',digest('bootstrap'))).fetchone()[0]
    b=c.execute('SELECT photo.billing_grant_bootstrap(%s,%s)',(t,g)).fetchone()[0]
    return g,b

def lease(c,t,p,resource='standard_photo',instance=None,generation=1,key=None):
    instance=instance or str(uuid4());key=key or str(uuid4())
    lid=c.execute('SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,%s,1,%s)',(t,resource,p,instance,generation,key)).fetchone()[0]
    return lid,c.execute('SELECT fence_token FROM photo.billing_admission_leases WHERE id=%s',(lid,)).fetchone()[0]

def issue(c,t,l,f,q=1,resource='standard_photo',key=None,hash_value=None):
    key=key or str(uuid4())
    return c.execute('SELECT photo.billing_issue_funding_claim(%s,%s,%s,%s,%s,%s,%s)',(t,resource,l,f,key,hash_value or digest(key),q)).fetchone()[0]

def finish(c,t,claim,q=1,resource='standard_photo',proof=None):
    return c.execute('SELECT photo.billing_finalize_funding_claim(%s,%s,%s,%s,%s)',(t,resource,claim,q,proof or digest('terminal/'+str(claim)))).fetchone()[0]

def close(c,t,l,f,resource='standard_photo'):
    return c.execute('SELECT photo.billing_close_admission_lease(%s,%s,%s,%s)',(t,resource,l,f)).fetchone()[0]

def rejects(c,sql,params=(),match=None,error=psycopg.Error):
    with pytest.raises(error,match=match):
        with c.transaction():c.execute(sql,params)

def check_conservation(c,t):
    for row in c.execute('SELECT id,original_quantity,remaining_quantity,leased_quantity,consumed_quantity,revoked_quantity,transferred_quantity FROM photo.billing_credit_buckets WHERE tenant_id=%s',(t,)):
        bid,original,remaining,leased,consumed,revoked,transferred=row
        assert original==remaining+leased+consumed+revoked+transferred
        assert min(row[1:])>=0
        # Rebuild balances from immutable journal semantics, independently of mutable counters.
        rebuilt=[0,0,0,0]
        for event,q,lease_id in c.execute('SELECT event_type,quantity,lease_id FROM photo.billing_resource_ledger WHERE tenant_id=%s AND bucket_id=%s',(t,bid)):
            if event=='grant':rebuilt[0]+=q
            elif event=='allocate':rebuilt[0]-=q;rebuilt[1]+=q
            elif event=='settle':rebuilt[1]-=q;rebuilt[2]+=q
            elif event=='release':rebuilt[1]-=q;rebuilt[0]+=q
            elif event=='expire':rebuilt[1 if lease_id else 0]-=q;rebuilt[3]+=q
            elif event=='overrun_cover':rebuilt[0]-=q;rebuilt[2]+=q
        assert rebuilt==[remaining,leased,consumed,revoked]
    for configured,outstanding,settled in c.execute('SELECT configured_quantity,outstanding_quantity,settled_quantity FROM photo.billing_allowance_budgets WHERE tenant_id=%s',(t,)):
        assert 0<=outstanding+settled<=configured
        assert outstanding>=0 and settled>=0
    for resource,overrun in c.execute('SELECT resource_code,overrun_quantity FROM photo.billing_resource_balances WHERE tenant_id=%s',(t,)):
        projected=c.execute("SELECT coalesce(sum(CASE WHEN event_type='overrun' THEN quantity WHEN event_type='overrun_cover' THEN -quantity ELSE 0 END),0) FROM photo.billing_resource_ledger WHERE tenant_id=%s AND resource_code=%s",(t,resource)).fetchone()[0]
        assert overrun==projected

def test_grant_lease_issue_success_replay_and_projection(env):
    c,t,p,_=env;g,b=grant(c,t,3)
    assert c.execute('SELECT photo.billing_grant_bootstrap(%s,%s)',(t,g)).fetchone()[0]==b
    l,f=lease(c,t,p,instance='process',key='lease')
    assert lease(c,t,p,instance='process',key='lease')==(l,f)
    claim=issue(c,t,l,f,2,key='request')
    assert issue(c,t,l,f,2,key='request')==claim
    finish(c,t,claim,2);finish(c,t,claim,2)
    close(c,t,l,f);close(c,t,l,f)
    assert c.execute('SELECT remaining_quantity,leased_quantity,consumed_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(1,0,2)
    summary=c.execute('SELECT available,allocated,consumed,overrun FROM photo.billing_project_quota(%s,%s)',(t,'standard_photo')).fetchone()
    assert summary==(1,0,2,0)
    check_conservation(c,t)

def test_failed_claim_returns_funding_and_closed_lease_drains(env):
    c,t,p,_=env;_,b=grant(c,t);l,f=lease(c,t,p);claim=issue(c,t,l,f)
    close(c,t,l,f)
    assert c.execute('SELECT state FROM photo.billing_admission_leases WHERE id=%s',(l,)).fetchone()==('draining',)
    assert c.execute('SELECT leased_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(1,)
    finish(c,t,claim,0)
    assert c.execute('SELECT state FROM photo.billing_admission_leases WHERE id=%s',(l,)).fetchone()==('closed',)
    assert c.execute('SELECT remaining_quantity,leased_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(3,0)
    check_conservation(c,t)

def test_settled_allowance_not_reset_by_restart_cover_or_new_generation(env):
    c,t,p,_=env;grant(c,t,3);l,f=lease(c,t,p,instance='process',key='generation1');claim=issue(c,t,l,f,4)
    finish(c,t,claim,4);close(c,t,l,f)
    assert c.execute('SELECT outstanding_quantity,settled_quantity FROM photo.billing_allowance_budgets WHERE tenant_id=%s',(t,)).fetchone()==(0,1)
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,2,1,%s)',(t,'standard_photo',p,'process','generation2'),'BILLING_OVERRUN_BLOCKED')
    assert c.execute('SELECT consumed,overrun FROM photo.billing_project_quota(%s,%s)',(t,'standard_photo')).fetchone()==(4,1)
    _,b=grant(c,t,20,trial=True)
    assert c.execute('SELECT consumed,overrun FROM photo.billing_project_quota(%s,%s)',(t,'standard_photo')).fetchone()==(4,0)
    assert c.execute('SELECT remaining_quantity,consumed_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(19,1)
    l2,f2=lease(c,t,p,instance='process',generation=2,key='generation2')
    assert c.execute('SELECT allowance_quantity FROM photo.billing_admission_leases WHERE id=%s',(l2,)).fetchone()==(0,)
    close(c,t,l2,f2)
    assert c.execute('SELECT settled_quantity FROM photo.billing_allowance_budgets WHERE tenant_id=%s',(t,)).fetchone()==(1,)
    check_conservation(c,t)

def test_unconsumed_allowance_can_be_reclaimed_but_never_double_spent(env):
    c,t,p,_=env;l,f=lease(c,t,p);claim=issue(c,t,l,f);close(c,t,l,f)
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,1,1,%s)',(t,'standard_photo',p,'other','other'),'BILLING_QUOTA_EXCEEDED')
    finish(c,t,claim,0)
    l2,f2=lease(c,t,p);close(c,t,l2,f2)
    assert c.execute('SELECT outstanding_quantity,settled_quantity FROM photo.billing_allowance_budgets WHERE tenant_id=%s',(t,)).fetchone()==(0,0)
    check_conservation(c,t)

def test_mixed_partial_egress_settlement(env):
    c,t,p,_=env;_,b=grant(c,t,10,resource='outgoing_bytes');l,f=lease(c,t,p,resource='outgoing_bytes');claim=issue(c,t,l,f,12,resource='outgoing_bytes')
    finish(c,t,claim,11,resource='outgoing_bytes');close(c,t,l,f,resource='outgoing_bytes')
    assert c.execute('SELECT remaining_quantity,leased_quantity,consumed_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(0,0,10)
    assert c.execute('SELECT outstanding_quantity,settled_quantity FROM photo.billing_allowance_budgets WHERE tenant_id=%s',(t,)).fetchone()==(0,1)
    check_conservation(c,t)

def test_expiry_blocks_issue_keeps_issued_and_revokes_late_failed_funding(env):
    c,t,p,_=env;_,b=grant(c,t);l,f=lease(c,t,p);claim=issue(c,t,l,f)
    c.execute("UPDATE photo.billing_admission_leases SET expires_at=now()-interval '1 second' WHERE id=%s",(l,))
    rejects(c,'SELECT photo.billing_issue_funding_claim(%s,%s,%s,%s,%s,%s,1)',(t,'standard_photo',l,f,'new',digest('new')),'BILLING_LEASE_EXPIRED')
    c.execute("UPDATE photo.billing_credit_buckets SET expires_at=now()-interval '1 second' WHERE id=%s",(b,))
    close(c,t,l,f);finish(c,t,claim,0)
    assert c.execute('SELECT remaining_quantity,leased_quantity,revoked_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(0,0,3)
    check_conservation(c,t)

def test_expiry_preserves_late_success_and_never_trial(env):
    c,t,p,_=env;_,b=grant(c,t);_,trial=grant(c,t,20,trial=True);l,f=lease(c,t,p);claim=issue(c,t,l,f,2)
    # Earliest finite expiry is consumed before the never-expiring trial bucket.
    assert c.execute('SELECT bucket_id FROM photo.billing_funding_claim_allocations ca JOIN photo.billing_lease_allocations a ON a.id=ca.lease_allocation_id WHERE claim_id=%s',(claim,)).fetchone()==(b,)
    c.execute("UPDATE photo.billing_credit_buckets SET expires_at=now()-interval '1 second' WHERE id=%s",(b,))
    close(c,t,l,f);finish(c,t,claim,2)
    assert c.execute('SELECT consumed_quantity,revoked_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(2,1)
    assert c.execute('SELECT photo.billing_expire_quota_buckets(%s,%s,100)',(t,'standard_photo')).fetchone()==(0,)
    assert c.execute('SELECT revoked_quantity FROM photo.billing_credit_buckets WHERE id=%s',(trial,)).fetchone()==(0,)
    check_conservation(c,t)

def test_bounded_expiry_and_replay(env):
    c,t,p,_=env;_,b=grant(c,t)
    c.execute("UPDATE photo.billing_credit_buckets SET expires_at=now()-interval '1 second' WHERE id=%s",(b,))
    assert c.execute('SELECT photo.billing_expire_quota_buckets(%s,%s,1)',(t,'standard_photo')).fetchone()==(1,)
    assert c.execute('SELECT photo.billing_expire_quota_buckets(%s,%s,1)',(t,'standard_photo')).fetchone()==(0,)
    check_conservation(c,t)

def test_conflicting_replay_fences_and_over_issue(env):
    c,t,p,_=env;grant(c,t);l,f=lease(c,t,p,instance='process',key='lease');claim=issue(c,t,l,f,key='claim')
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,1,1,%s)',(t,'standard_photo',p,'other','lease'),'BILLING_IDEMPOTENCY_CONFLICT')
    rejects(c,'SELECT photo.billing_issue_funding_claim(%s,%s,%s,%s,%s,%s,2)',(t,'standard_photo',l,f,'claim',digest('claim')),'BILLING_IDEMPOTENCY_CONFLICT')
    rejects(c,'SELECT photo.billing_issue_funding_claim(%s,%s,%s,%s,%s,%s,1)',(t,'standard_photo',l,uuid4(),'new',digest('new')),'BILLING_FENCE_INVALID')
    rejects(c,'SELECT photo.billing_issue_funding_claim(%s,%s,%s,%s,%s,%s,100)',(t,'standard_photo',l,f,'new',digest('new')),'BILLING_QUOTA_EXCEEDED')
    finish(c,t,claim)
    rejects(c,'SELECT photo.billing_finalize_funding_claim(%s,%s,%s,0,%s)',(t,'standard_photo',claim,digest('failure')),'BILLING_TERMINAL_CONFLICT')
    check_conservation(c,t)

def test_period_pin_generation_bounds_and_shadow_only(env):
    c,t,p,_=env;grant(c,t);l,f=lease(c,t,p,instance='process')
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,2,1,%s)',(t,'standard_photo',p,'process','another'),'BILLING_LOCAL_LEASE_LIMIT')
    close(c,t,l,f)
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,1,1,%s)',(t,'standard_photo',p,'process','stale'),'BILLING_STALE_GENERATION')
    rejects(c,'UPDATE photo.billing_allowance_budgets SET configured_quantity=configured_quantity+1',error=psycopg.errors.CheckViolation)
    c.execute("INSERT INTO photo.billing_admission_policies(resource_code,policy_version,chunk_quantity,lease_seconds,cache_ttl_seconds,optimistic_allowance,max_local_leases,cold_timeout_ms) VALUES ('standard_photo',2,5,10,5,1,1,200)")
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,2,2,%s)',(t,'standard_photo',p,'process','newpolicy'),'BILLING_PERIOD_POLICY_PINNED')
    rejects(c,"UPDATE photo.billing_accounting_controls SET mode='enforce'",error=psycopg.errors.CheckViolation)
    rejects(c,'SELECT photo.billing_expire_quota_buckets(%s,%s,0)',(t,'standard_photo'),'BILLING_BATCH_INVALID')
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,1,1,%s)',(t,'storage_bytes',p,'instance','storage'),'BILLING_RESOURCE_INVALID')

def test_lifecycle_and_cross_tenant_revalidation(env):
    c,t,p,_=env;g,b=grant(c,t);l,f=lease(c,t,p);claim=issue(c,t,l,f)
    other=tenant(c);c.execute('INSERT INTO photo.billing_accounting_controls(tenant_id) VALUES (%s)',(other,))
    rejects(c,'SELECT photo.billing_grant_bootstrap(%s,%s)',(other,g),'BILLING_GRANT_INVALID')
    rejects(c,'SELECT photo.billing_close_admission_lease(%s,%s,%s,%s)',(other,'standard_photo',l,f),'BILLING_FENCE_INVALID')
    rejects(c,'SELECT photo.billing_finalize_funding_claim(%s,%s,%s,1,%s)',(other,'standard_photo',claim,digest('proof')),'BILLING_CLAIM_INVALID')
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,1,1,%s)',(other,'standard_photo',p,'other','bad-period'),'BILLING_PERIOD_INVALID')
    c.execute("UPDATE photo.billing_accounting_controls SET lifecycle='deleting' WHERE tenant_id=%s",(t,))
    rejects(c,'SELECT photo.billing_grant_bootstrap(%s,%s)',(t,g),'BILLING_ACCOUNTING_DISABLED')
    rejects(c,'SELECT photo.billing_finalize_funding_claim(%s,%s,%s,1,%s)',(t,'standard_photo',claim,digest('proof')),'BILLING_ACCOUNTING_DISABLED')
    c.execute("UPDATE photo.billing_accounting_controls SET lifecycle='active' WHERE tenant_id=%s",(t,));c.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(t,))
    rejects(c,'SELECT photo.billing_grant_bootstrap(%s,%s)',(t,g),'BILLING_TENANT_INACTIVE')

def test_no_public_or_runtime_execute_and_ledger_immutable(env):
    c,t,p,_=env;grant(c,t)
    for role in ['photo_api','photo_worker','photo_maintenance','photo_auth','photo_portal','photo_scheduler','photo_publisher']:
        with c.transaction():
            c.execute('SET LOCAL ROLE '+role)
            rejects(c,'SELECT photo.billing_quota_lock(%s,%s)',(t,'standard_photo'),error=psycopg.errors.InsufficientPrivilege)
            rejects(c,'SELECT * FROM photo.billing_resource_ledger',error=psycopg.errors.InsufficientPrivilege)
        assert c.execute("SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname LIKE 'billing_%%' AND has_function_privilege(%s,p.oid,'EXECUTE')",(role,)).fetchone()==(0,)
    rejects(c,"UPDATE photo.billing_resource_ledger SET quantity=quantity+1",error=psycopg.errors.CheckViolation)
    rejects(c,"DELETE FROM photo.billing_resource_ledger",error=psycopg.errors.CheckViolation)
    bad=c.execute("SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname LIKE 'billing_%' AND (NOT p.prosecdef OR pg_get_userbyid(p.proowner)<>'photo_fn_owner' OR NOT 'search_path=pg_catalog, photo'=ANY(p.proconfig) AND NOT 'search_path=pg_catalog,photo'=ANY(p.proconfig))").fetchall()
    assert bad==[]
    assert c.execute("SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE n.nspname='photo' AND p.proname LIKE 'billing_%' AND a.grantee=0 AND a.privilege_type='EXECUTE'").fetchone()==(0,)

def race(config,calls):
    barrier=Barrier(len(calls))
    def run(call):
        with psycopg.connect(**config,autocommit=True) as cx:
            cx.execute("SET statement_timeout='8s'");cx.execute("SET lock_timeout='6s'");cx.execute('SET ROLE photo_fn_owner')
            barrier.wait(timeout=10)
            try:
                with cx.transaction():return ('ok',call(cx))
            except psycopg.Error as e:return ('error',e.sqlstate,str(e).splitlines()[0])
    with ThreadPoolExecutor(max_workers=len(calls)) as pool:return list(pool.map(run,calls))

def test_concurrent_last_paid_and_period_allowance_are_bounded(env):
    c,t,p,config=env;grant(c,t,1)
    results=race(config,[lambda cx,i=i:lease(cx,t,p,instance='process'+str(i))[0] for i in range(8)])
    assert len([r for r in results if r[0]=='ok'])==1
    assert all(r[0]=='ok' or 'BILLING_QUOTA_EXCEEDED' in r[2] for r in results)
    assert c.execute('SELECT sum(allocated_quantity),sum(allowance_quantity) FROM photo.billing_admission_leases').fetchone()==(1,1)
    check_conservation(c,t)

def test_concurrent_claim_limit_and_same_request_replay(env):
    c,t,p,config=env;grant(c,t,1);l,f=lease(c,t,p)
    results=race(config,[lambda cx,i=i:issue(cx,t,l,f,key='claim'+str(i)) for i in range(8)])
    assert len([r for r in results if r[0]=='ok'])==2
    assert all(r[0]=='ok' or 'BILLING_QUOTA_EXCEEDED' in r[2] for r in results)
    assert c.execute('SELECT sum(quantity) FROM photo.billing_funding_claims').fetchone()==(2,)
    # Exact duplicate issue must be one claim, even across connections.
    first=c.execute('SELECT request_key,id FROM photo.billing_funding_claims ORDER BY request_key LIMIT 1').fetchone()
    repeats=race(config,[lambda cx:issue(cx,t,l,f,key=first[0]) for _ in range(6)])
    assert all(r==('ok',first[1]) for r in repeats)
    check_conservation(c,t)

def test_concurrent_success_vs_failure_and_close_preserve_funding(env):
    c,t,p,config=env;_,b=grant(c,t);l,f=lease(c,t,p);claim=issue(c,t,l,f)
    results=race(config,[lambda cx:finish(cx,t,claim,1),lambda cx:finish(cx,t,claim,0),lambda cx:close(cx,t,l,f)])
    assert len([r for r in results if r[0]=='ok'])==2
    assert len([r for r in results if r[0]=='error' and 'BILLING_TERMINAL_CONFLICT' in r[2]])==1
    assert c.execute('SELECT leased_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(0,)
    assert c.execute('SELECT state FROM photo.billing_admission_leases WHERE id=%s',(l,)).fetchone()==('closed',)
    check_conservation(c,t)

def test_concurrent_duplicate_bootstrap_and_lease(env):
    c,t,p,config=env
    g=c.execute("INSERT INTO photo.billing_bootstrap_grants(tenant_id,resource_code,reason,quantity,starts_at,expiry_policy,policy_snapshot_hash) VALUES (%s,'standard_photo','trial_once',20,now()-interval '1 hour','never',%s) RETURNING id",(t,digest('initial'))).fetchone()[0]
    results=race(config,[lambda cx:cx.execute('SELECT photo.billing_grant_bootstrap(%s,%s)',(t,g)).fetchone()[0] for _ in range(6)])
    assert results[0][0]=='ok' and all(r==results[0] for r in results)
    leases=race(config,[lambda cx:lease(cx,t,p,instance='same',key='same')[0] for _ in range(6)])
    assert all(r==leases[0] for r in leases) and leases[0][0]=='ok'
    assert c.execute("SELECT count(*) FROM photo.billing_resource_ledger WHERE event_type='grant'").fetchone()==(1,)
    check_conservation(c,t)

@pytest.mark.migration_head(2601)
def test_quota_full_migration_head(database):
    with psycopg.connect(**database) as c:
        assert c.execute('SELECT count(*) FROM photo.billing_accounting_controls').fetchone()==(0,)
        assert c.execute('SELECT count(*) FROM photo.billing_resource_ledger').fetchone()==(0,)
        assert c.execute("SELECT count(*) FROM photo.billing_provider_accounts WHERE environment<>'test'").fetchone()==(0,)
        # Exercise the real kernel after all current schema/security changes, not just DDL.
        t=tenant(c)
        c.execute('INSERT INTO photo.billing_accounting_controls(tenant_id) VALUES (%s)',(t,))
        period=c.execute("INSERT INTO photo.billing_periods(tenant_id,starts_at,ends_at,anchor_kind) VALUES (%s,now()-interval '1 hour',now()+interval '1 hour','trial_calendar') RETURNING id",(t,)).fetchone()[0]
        grant(c,t,20,trial=True);l,f=lease(c,t,period);claim=issue(c,t,l,f)
        finish(c,t,claim);close(c,t,l,f);check_conservation(c,t)
        assert c.execute('SELECT available,consumed FROM photo.billing_project_quota(%s,%s)',(t,'standard_photo')).fetchone()==(19,1)

@pytest.mark.parametrize('quantity',[None,0,-1,9223372036854775807])
def test_invalid_issue_quantities_leave_no_partial_rows(env,quantity):
    c,t,p,_=env;grant(c,t);l,f=lease(c,t,p)
    rejects(c,'SELECT photo.billing_issue_funding_claim(%s,%s,%s,%s,%s,%s,%s)',(t,'standard_photo',l,f,'invalid',digest('invalid'),quantity))
    assert c.execute('SELECT count(*) FROM photo.billing_funding_claims').fetchone()==(0,)
    assert c.execute('SELECT issued_quantity FROM photo.billing_admission_leases WHERE id=%s',(l,)).fetchone()==(0,)
    check_conservation(c,t)

@pytest.mark.parametrize('confirmed',[None,-1,2,4])
def test_invalid_terminal_quantities_preserve_accepted_claim(env,confirmed):
    c,t,p,_=env;grant(c,t);l,f=lease(c,t,p);claim=issue(c,t,l,f,3)
    rejects(c,'SELECT photo.billing_finalize_funding_claim(%s,%s,%s,%s,%s)',(t,'standard_photo',claim,confirmed,digest('proof')))
    assert c.execute('SELECT state FROM photo.billing_funding_claims WHERE id=%s',(claim,)).fetchone()==('accepted',)
    check_conservation(c,t)

def test_transaction_rollback_removes_all_escrow_and_events(env):
    c,t,p,_=env;grant(c,t)
    with pytest.raises(RuntimeError):
        with c.transaction():
            l,f=lease(c,t,p);issue(c,t,l,f);raise RuntimeError('caller admission failed')
    assert c.execute('SELECT count(*) FROM photo.billing_admission_leases').fetchone()==(0,)
    assert c.execute('SELECT count(*) FROM photo.billing_funding_claims').fetchone()==(0,)
    assert c.execute('SELECT count(*) FROM photo.billing_allowance_budgets').fetchone()==(0,)
    assert c.execute('SELECT count(*) FROM photo.billing_resource_ledger').fetchone()==(1,)
    check_conservation(c,t)

def test_lease_is_capped_by_bucket_and_accounting_period(env):
    c,t,p,_=env;_,b=grant(c,t)
    c.execute("UPDATE photo.billing_credit_buckets SET expires_at=now()+interval '5 seconds' WHERE id=%s",(b,))
    c.execute("UPDATE photo.billing_periods SET ends_at=now()+interval '3 seconds' WHERE id=%s",(p,))
    l,f=lease(c,t,p)
    assert c.execute('SELECT l.expires_at<=p.ends_at AND l.expires_at<=b.expires_at FROM photo.billing_admission_leases l JOIN photo.billing_periods p ON p.id=l.period_id CROSS JOIN photo.billing_credit_buckets b WHERE l.id=%s AND b.id=%s',(l,b)).fetchone()==(True,)
    check_conservation(c,t)

def test_recovery_cannot_return_issued_funding_after_lease_expiry(env):
    c,t,p,_=env;grant(c,t);l,f=lease(c,t,p);claim=issue(c,t,l,f,4)
    c.execute("UPDATE photo.billing_admission_leases SET expires_at=now()-interval '1 second' WHERE id=%s",(l,))
    close(c,t,l,f)
    assert c.execute('SELECT outstanding_quantity FROM photo.billing_allowance_budgets WHERE tenant_id=%s',(t,)).fetchone()==(1,)
    rejects(c,'SELECT photo.billing_allocate_admission_lease(%s,%s,%s,%s,1,1,%s)',(t,'standard_photo',p,'restart','restart'),'BILLING_QUOTA_EXCEEDED')
    finish(c,t,claim,4)
    rejects(c,'UPDATE photo.billing_allowance_budgets SET settled_quantity=0',error=psycopg.errors.CheckViolation)
    check_conservation(c,t)

def test_concurrent_retry_settles_once_and_cannot_mint_trial(env):
    c,t,p,config=env;grant(c,t,20,trial=True);l,f=lease(c,t,p);claim=issue(c,t,l,f)
    results=race(config,[lambda cx:finish(cx,t,claim) for _ in range(8)])
    assert all(r==('ok',claim) for r in results)
    assert c.execute("SELECT sum(quantity) FROM photo.billing_resource_ledger WHERE event_type='settle'").fetchone()==(1,)
    rejects(c,"INSERT INTO photo.billing_bootstrap_grants(tenant_id,resource_code,reason,quantity,starts_at,expiry_policy,policy_snapshot_hash) VALUES (%s,'standard_photo','trial_once',20,now(),'never',%s)",(t,digest('another')),error=psycopg.errors.UniqueViolation)
    check_conservation(c,t)

def test_concurrent_expire_close_and_terminal_have_no_deadlock(env):
    c,t,p,config=env;_,b=grant(c,t,10);l,f=lease(c,t,p);claim=issue(c,t,l,f)
    c.execute("UPDATE photo.billing_credit_buckets SET expires_at=now()-interval '1 second' WHERE id=%s",(b,))
    results=race(config,[lambda cx:finish(cx,t,claim),lambda cx:close(cx,t,l,f),lambda cx:cx.execute('SELECT photo.billing_expire_quota_buckets(%s,%s,100)',(t,'standard_photo')).fetchone()[0]])
    assert all(r[0]=='ok' for r in results)
    assert c.execute('SELECT leased_quantity,consumed_quantity,revoked_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(0,1,9)
    check_conservation(c,t)

def test_cross_tenant_allocation_fk_cannot_be_spoofed(env):
    c,t,p,_=env;grant(c,t);l,f=lease(c,t,p);claim=issue(c,t,l,f)
    other=tenant(c)
    rejects(c,'UPDATE photo.billing_funding_claim_allocations SET tenant_id=%s',(other,),error=psycopg.errors.CheckViolation)
    # An insertion with all genuine IDs still cannot combine a foreign tenant with this claim.
    rejects(c,'INSERT INTO photo.billing_funding_claim_allocations(tenant_id,resource_code,period_id,lease_id,claim_id,lease_allocation_id,quantity) SELECT %s,resource_code,period_id,lease_id,claim_id,lease_allocation_id,quantity FROM photo.billing_funding_claim_allocations LIMIT 1',(other,),error=psycopg.errors.ForeignKeyViolation)
    check_conservation(c,t)

def test_draining_commitments_do_not_block_refill_but_do_keep_funding(env):
    c,t,p,_=env;_,b=grant(c,t,10);l,f=lease(c,t,p,instance='same',generation=1);claim=issue(c,t,l,f)
    close(c,t,l,f)
    l2,f2=lease(c,t,p,instance='same',generation=2)
    assert c.execute('SELECT state FROM photo.billing_admission_leases WHERE id=%s',(l,)).fetchone()==('draining',)
    assert c.execute('SELECT leased_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(6,)
    finish(c,t,claim);close(c,t,l2,f2)
    assert c.execute('SELECT remaining_quantity,consumed_quantity,leased_quantity FROM photo.billing_credit_buckets WHERE id=%s',(b,)).fetchone()==(9,1,0)
    check_conservation(c,t)
