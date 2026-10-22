"""Real standard Apply admission, canonical adoption, terminal release and PostgreSQL races."""
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
from test_apply_adoption_atomicity import _adoptable
from test_billing_quota import check_conservation,grant,race,rejects

pytestmark=pytest.mark.migration_head(2607)

def enroll(c,t,quantity=20):
    c.execute('INSERT INTO photo.billing_accounting_controls(tenant_id) VALUES (%s)',(t,))
    c.execute('INSERT INTO photo.billing_workflow_controls(tenant_id,enabled) VALUES (%s,true)',(t,))
    c.execute("INSERT INTO photo.billing_periods(tenant_id,starts_at,ends_at,anchor_kind) VALUES (%s,now()-interval '1 hour',now()+interval '1 day','trial_calendar')",(t,))
    if quantity:grant(c,t,quantity,trial=quantity==20)

def bind(c,t,run,token,items,output,quantity=20,key=None,enable=True,instance='api-test'):
    if enable and not c.execute('SELECT 1 FROM photo.billing_workflow_controls WHERE tenant_id=%s',(t,)).fetchone():enroll(c,t,quantity)
    actor=c.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    session=c.execute('SELECT id FROM photo.sessions WHERE user_id=%s AND expires_at>clock_timestamp() LIMIT 1',(actor,)).fetchone()
    session=session[0] if session else c.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES (%s,%s,%s,'native',now()+interval '1 hour') RETURNING id",(actor,sha256(str(uuid4()).encode()).digest(),uuid4())).fetchone()[0]
    key=key or str(run)
    c.execute("SELECT set_config('app.tenant_id',%s,true)",(str(t),))
    assert c.execute("SELECT photo.billing_prepare_apply(%s,%s,%s,'Run.Apply')",(t,actor,session)).fetchone()==(True,)
    return c.execute('SELECT photo.billing_bind_apply(%s,%s,%s,%s,%s,%s,%s)',(t,actor,session,run,key,sha256(key.encode()).digest(),instance)).fetchone()[0]

def next_build(t,first_run,items):
    def build(c):
        r=c.execute('SELECT input_snapshot_id,created_by FROM photo.runs WHERE id=%s',(first_run,)).fetchone()
        source=c.execute('SELECT source_collection_id FROM photo.apply_builds WHERE run_id=%s',(first_run,)).fetchone()[0]
        run=c.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,'apply','queued',%s,'{}',%s,%s) RETURNING id",(t,r[0],'b'*64,r[1])).fetchone()[0]
        token=uuid4();c.execute("INSERT INTO photo.apply_builds(tenant_id,run_id,source_collection_id,item_count,phase,graph_version,lease_token,lease_until) VALUES(%s,%s,%s,%s,'resolve',4,%s,clock_timestamp()+interval '2 minutes')",(t,run,source,len(items),token))
        return t,run,token,items
    return build

def adopt(c,t,r,token):return c.execute('SELECT photo.adopt_apply_results(%s,%s,%s)',(t,r,token)).fetchone()[0]

def consumed(c,t):return c.execute('SELECT consumed,overrun FROM photo.billing_project_quota(%s,%s)',(t,'standard_photo')).fetchone()

@pytest.mark.parametrize('graph',[3,4])
def test_real_apply_consumes_only_canonical_results_once(db,graph):
    t,r,token,items,out=_adoptable(db,graph,admit=bind)
    import json
    policy=dict(schema_version=1,policy_version=1,included_reruns=2,rerun_window_hours=168,success_contract='apply_final_adoption_v1')
    expected_hash=sha256(json.dumps(policy,sort_keys=True,separators=(',',':')).encode()).digest()
    assert db.execute("SELECT policy_snapshot_hash FROM photo.billing_operation_policies WHERE id='a2e8d020-bcc2-5bfa-bab4-f942c678b301'").fetchone()==(expected_hash,)
    assert consumed(db,t)==(0,0)
    assert not adopt(db,t,r,uuid4())
    assert consumed(db,t)==(0,0)
    assert adopt(db,t,r,token)
    assert consumed(db,t)==(2,0)
    assert db.execute('SELECT count(*) FROM photo.billing_result_references WHERE tenant_id=%s',(t,)).fetchone()==(2,)
    assert not adopt(db,t,r,token)
    assert consumed(db,t)==(2,0)
    check_conservation(db,t)

def test_adoption_refusal_rolls_back_partial_outputs_and_billing(db):
    t,r,token,items,out=_adoptable(db,4,admit=bind)
    db.execute('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(items[1][1],))
    assert not adopt(db,t,r,token)
    assert consumed(db,t)==(0,0)
    assert db.execute('SELECT count(*) FROM photo.billing_result_references WHERE tenant_id=%s',(t,)).fetchone()==(0,)
    assert db.execute('SELECT count(*) FROM photo.asset_versions WHERE tenant_id=%s AND created_by_run_id=%s',(t,r)).fetchone()==(0,)
    db.execute('UPDATE photo.assets SET deleted_at=NULL WHERE id=%s',(items[1][1],))
    assert adopt(db,t,r,token)
    assert consumed(db,t)==(2,0)
    check_conservation(db,t)

def test_final_failure_returns_once_and_cannot_resurrect_refunded_run(db):
    t,r,token,items,out=_adoptable(db,4,admit=bind)
    assert not db.execute("SELECT photo.fail_apply_build(%s,%s,'FAILED')",(r,uuid4())).fetchone()[0]
    assert db.execute("SELECT photo.fail_apply_build(%s,%s,'FAILED')",(r,token)).fetchone()[0]
    assert consumed(db,t)==(0,0)
    assert db.execute("SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s AND state='released'",(t,)).fetchone()==(2,)
    assert not db.execute("SELECT photo.fail_apply_build(%s,%s,'FAILED')",(r,token)).fetchone()[0]
    assert not adopt(db,t,r,token)
    rejects(db,"UPDATE photo.runs SET status='running' WHERE id=%s",(r,),'BILLING_RUN_TERMINAL',psycopg.errors.CheckViolation)
    check_conservation(db,t)

def test_cancellation_finalization_returns_funding_and_removes_initial_hold(db):
    t,r,token,items,out=_adoptable(db,4,admit=bind)
    db.execute("UPDATE photo.runs SET cancel_requested=true,status='cancelling' WHERE id=%s",(r,))
    assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(1,)
    assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(0,)
    assert consumed(db,t)==(0,0)
    assert db.execute("SELECT count(*) FROM photo.billing_photo_free_slots WHERE tenant_id=%s AND state='held'",(t,)).fetchone()==(0,)
    check_conservation(db,t)

def test_batch_known_deficit_is_atomic(db):
    with pytest.raises(psycopg.Error,match='BILLING_QUOTA_EXCEEDED'):
        with db.transaction():_adoptable(db,4,admit=lambda *a:bind(*a,quantity=1))
    assert db.execute('SELECT count(*) FROM photo.billing_usage_requests').fetchone()==(0,)
    assert db.execute('SELECT count(*) FROM photo.billing_funding_claims').fetchone()==(0,)
    assert db.execute('SELECT count(*) FROM photo.tasks').fetchone()==(0,)

def test_two_free_reruns_do_not_charge_and_failure_returns_slot(db):
    t,r,token,items,_=_adoptable(db,4,admit=bind,count=1);assert adopt(db,t,r,token)
    factory=next_build(t,r,items)
    t,r2,k2,_,_=_adoptable(db,4,admit=bind,build=factory)
    assert db.execute("SELECT max_quantity FROM photo.billing_usage_charge_items ci JOIN photo.billing_usage_requests req ON req.tenant_id=ci.tenant_id AND req.id=ci.usage_request_id WHERE req.run_id=%s",(r2,)).fetchone()==(0,)
    assert db.execute("SELECT photo.fail_apply_build(%s,%s,'FAILED')",(r2,k2)).fetchone()[0]
    for _ in range(2):
        t,rr,kk,_,_=_adoptable(db,4,admit=bind,build=factory);assert adopt(db,t,rr,kk)
    assert consumed(db,t)==(1,0)
    t,last,key,_,_=_adoptable(db,4,admit=bind,build=factory);assert adopt(db,t,last,key)
    assert consumed(db,t)==(2,0)
    check_conservation(db,t)

def test_initial_inflight_blocks_independent_request_but_technical_retry_is_free(db):
    t,r,k,items,_=_adoptable(db,4,admit=bind,count=1)
    with pytest.raises(psycopg.Error,match='PHOTO_INITIAL_IN_PROGRESS'):
        with db.transaction():_adoptable(db,4,admit=bind,build=next_build(t,r,items))
    # Technical scheduler attempts do not touch root admission or manufacture new tickets.
    assert not adopt(db,t,r,uuid4());assert adopt(db,t,r,k)
    assert db.execute('SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==(1,)
    check_conservation(db,t)

def test_single_request_allowance_then_free_rerun_even_when_overrun(db):
    t,r,k,items,_=_adoptable(db,4,count=1,admit=lambda *a:bind(*a,quantity=0));assert adopt(db,t,r,k)
    assert consumed(db,t)==(1,1)
    t,rr,kk,_,_=_adoptable(db,4,admit=bind,build=next_build(t,r,items));assert adopt(db,t,rr,kk)
    assert consumed(db,t)==(1,1)
    t,rr,kk,_,_=_adoptable(db,4,admit=bind,build=next_build(t,r,items));assert adopt(db,t,rr,kk)
    with pytest.raises(psycopg.Error,match='BILLING_OVERRUN_BLOCKED'):
        with db.transaction():_adoptable(db,4,admit=bind,build=next_build(t,r,items))
    assert consumed(db,t)==(1,1)
    check_conservation(db,t)

def test_api_cannot_finalize_or_bypass_wrapper(db):
    for role,name in [('photo_api','billing_finalize_funding_claim(uuid,text,uuid,bigint,bytea)'),('photo_api','billing_settle_apply_run(uuid,uuid)'),('photo_scheduler','adopt_apply_results_before_billing(uuid,uuid,uuid)'),('photo_scheduler','fail_apply_build_before_billing(uuid,uuid,text)')]:
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,'photo.'+name,'EXECUTE')).fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_scheduler','photo.adopt_apply_results(uuid,uuid,uuid)','EXECUTE')").fetchone()[0]

def test_duplicate_binding_and_current_authority(db):
    captured={}
    def callback(*args):captured['id']=bind(*args,key='same')
    t,r,k,items,out=_adoptable(db,4,admit=callback)
    assert bind(db,t,r,k,items,out,key='same')==captured['id']
    db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE user_id=(SELECT created_by FROM photo.runs WHERE id=%s)",(r,))
    # The same request cannot bypass revoked current authority through the binding replay.
    rejects(db,'SELECT photo.billing_bind_apply(%s,(SELECT created_by FROM photo.runs WHERE id=%s),(SELECT session_id FROM photo.billing_usage_requests WHERE run_id=%s),%s,%s,%s,%s)',(t,r,r,r,'same',sha256(b'same').digest(),'api-test'),'BILLING_AUTHORITY_INVALID')

def test_concurrent_adoption_vs_failure_is_atomic(database):
    with psycopg.connect(**database,autocommit=True) as c:
        with c.transaction():t,r,k,_,_=_adoptable(c,4,admit=bind)
        result=race(database,[lambda cx:adopt(cx,t,r,k),lambda cx:cx.execute("SELECT photo.fail_apply_build(%s,%s,'FAILED')",(r,k)).fetchone()[0]])
        assert sorted(x[1] for x in result if x[0]=='ok')==[False,True]
        status=c.execute('SELECT status FROM photo.runs WHERE id=%s',(r,)).fetchone()[0]
        assert consumed(c,t)==((2,0) if status=='completed' else (0,0))
        check_conservation(c,t)

def test_concurrent_duplicate_adoption_never_double_spends(database):
    with psycopg.connect(**database,autocommit=True) as c:
        with c.transaction():t,r,k,_,_=_adoptable(c,4,admit=bind)
        result=race(database,[lambda cx:adopt(cx,t,r,k) for _ in range(6)])
        assert all(x[0]=='ok' for x in result)
        assert sum(x[1] for x in result)==1
        assert consumed(c,t)==(2,0)
        check_conservation(c,t)

def test_real_go_apply_api_admission_replay_cancel_and_revocation(database):
    import json,os,subprocess
    from pathlib import Path
    from psycopg.conninfo import make_conninfo
    from test_apply_v4_plan import _build
    root=Path(__file__).resolve().parents[2]
    with psycopg.connect(**database,autocommit=True) as c:
        with c.transaction():
            t,run,_,_=_build(c,count=1);enroll(c,t)
            actor,epoch=c.execute('SELECT r.created_by,u.auth_epoch FROM photo.runs r JOIN photo.users u ON u.id=r.created_by WHERE r.id=%s',(run,)).fetchone()
            project,collection,revision=c.execute('SELECT c.project_id,c.id,c.revision FROM photo.apply_builds b JOIN photo.collections c ON c.tenant_id=b.tenant_id AND c.id=b.source_collection_id WHERE b.run_id=%s',(run,)).fetchone()
            session=c.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES (%s,%s,%s,'native',now()+interval '1 hour') RETURNING id",(actor,sha256(str(uuid4()).encode()).digest(),uuid4())).fetchone()[0]
    fixture=dict(Tenant=str(t),Actor=str(actor),Session=str(session),Project=str(project),Collection=str(collection),Epoch=epoch,Revision=revision)
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_BILLING_APPLY_FIXTURE=json.dumps(fixture))
    result=subprocess.run(['/usr/local/go/bin/go','test','./internal/run','-run','^TestIntegrationBillingApplyAdmission$','-count=1','-v'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert '--- PASS: TestIntegrationBillingApplyAdmission' in result.stdout
    with psycopg.connect(**database) as c:check_conservation(c,t)

def test_free_slot_decision_is_pinned_even_if_another_request_releases_slot(db):
    t,r,k,items,_=_adoptable(db,4,admit=bind,count=1);assert adopt(db,t,r,k)
    factory=next_build(t,r,items)
    t,one,k1,_,_=_adoptable(db,4,admit=bind,build=factory)
    t,two,k2,_,_=_adoptable(db,4,admit=bind,build=factory)
    t,paid,kp,_,_=_adoptable(db,4,admit=bind,build=factory)
    assert db.execute("SELECT ci.max_quantity FROM photo.billing_usage_charge_items ci JOIN photo.billing_usage_requests q ON q.id=ci.usage_request_id WHERE q.run_id=%s",(paid,)).fetchone()==(1,)
    assert db.execute("SELECT photo.fail_apply_build(%s,%s,'FAILED')",(one,k1)).fetchone()[0]
    assert adopt(db,t,two,k2);assert adopt(db,t,paid,kp)
    assert consumed(db,t)==(2,0)
    check_conservation(db,t)

def test_concurrent_initial_requests_cannot_create_two_paid_first_results(database):
    from test_apply_v4_plan import _build
    with psycopg.connect(**database,autocommit=True) as c:
        with c.transaction():
            t,base,_,items=_build(c,count=1);enroll(c,t)
            actor=c.execute('SELECT created_by FROM photo.runs WHERE id=%s',(base,)).fetchone()[0]
            c.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES (%s,%s,%s,'native',now()+interval '1 hour')",(actor,sha256(b'concurrent').digest(),uuid4()))
            runs=[]
            project=c.execute('SELECT project_id FROM photo.collections WHERE id=(SELECT source_collection_id FROM photo.apply_builds WHERE run_id=%s)',(base,)).fetchone()[0]
            for _ in range(2):
                _,rr,kk,_=next_build(t,base,items)(c)
                out=c.execute("INSERT INTO photo.collections(tenant_id,project_id,name) VALUES (%s,%s,'output') RETURNING id",(t,project)).fetchone()[0]
                c.execute('UPDATE photo.runs SET output_collection_id=%s WHERE id=%s',(out,rr));runs.append((rr,kk,out))
        results=race(database,[lambda cx,rr=rr,kk=kk,out=out:bind(cx,t,rr,kk,items,out) for rr,kk,out in runs])
        assert sum(r[0]=='ok' for r in results)==1
        assert sum(r[0]=='error' and 'PHOTO_INITIAL_IN_PROGRESS' in r[2] for r in results)==1
        assert c.execute('SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==(1,)
        check_conservation(c,t)


def test_binding_replay_under_fresh_valid_session_preserves_financial_identity(db):
    captured={}
    def callback(*args):captured['id']=bind(*args,key='stable-user-key')
    t,r,k,items,out=_adoptable(db,4,admit=callback)
    actor=db.execute('SELECT created_by FROM photo.runs WHERE id=%s',(r,)).fetchone()[0]
    session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES (%s,%s,%s,'native',now()+interval '1 hour') RETURNING id",(actor,sha256(b'new-auth').digest(),uuid4())).fetchone()[0]
    assert db.execute('SELECT photo.billing_bind_apply(%s,%s,%s,%s,%s,%s,%s)',(t,actor,session,r,'stable-user-key',sha256(b'stable-user-key').digest(),'api-test')).fetchone()==(captured['id'],)
    assert db.execute('SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==(2,)
    check_conservation(db,t)


def test_restart_reclaims_only_unissued_expired_lease_without_resetting_budget(db):
    t,r,k,items,_=_adoptable(db,4,admit=bind,count=1);assert adopt(db,t,r,k)
    factory=next_build(t,r,items)
    for _ in range(2):
        t,rr,kk,_,_=_adoptable(db,4,admit=bind,build=factory);assert adopt(db,t,rr,kk)
    old=db.execute("SELECT id FROM photo.billing_admission_leases WHERE tenant_id=%s AND instance_id='api-test'",(t,)).fetchone()[0]
    db.execute("UPDATE photo.billing_admission_leases SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(old,))
    t,rr,kk,_,_=_adoptable(db,4,admit=lambda *a:bind(*a,instance='after-restart'),build=factory)
    assert db.execute('SELECT state,returned_quantity FROM photo.billing_admission_leases WHERE id=%s',(old,)).fetchone()==('closed',4)
    assert adopt(db,t,rr,kk)
    assert consumed(db,t)==(2,0)
    assert db.execute('SELECT count(*),max(configured_quantity),max(settled_quantity) FROM photo.billing_allowance_budgets WHERE tenant_id=%s',(t,)).fetchone()==(1,1,0)
    check_conservation(db,t)
