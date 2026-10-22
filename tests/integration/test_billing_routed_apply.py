"""Routed admission against real canonical adoption and PostgreSQL transaction races.
Synthetic scene/run fixtures; no pixel execution or Stripe network calls.
"""
import json
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
import test_billing_apply as base
from test_billing_apply import enroll,adopt,consumed
from test_apply_adoption_atomicity import _adoptable
from test_billing_quota import grant,check_conservation,rejects,race
pytestmark=pytest.mark.migration_head(2607)

def routed_bind(c,t,run,token,items,output,quantity=20,key=None,enable=True,instance='api-test',route='SessionAnalysis.Apply'):
    if enable and not c.execute('SELECT 1 FROM photo.billing_workflow_controls WHERE tenant_id=%s',(t,)).fetchone():enroll(c,t,quantity)
    actor=c.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    session=c.execute('SELECT id FROM photo.sessions WHERE user_id=%s AND expires_at>clock_timestamp() LIMIT 1',(actor,)).fetchone()
    session=session[0] if session else c.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES (%s,%s,%s,'native',now()+interval '1 hour') RETURNING id",(actor,sha256(str(uuid4()).encode()).digest(),uuid4())).fetchone()[0]
    config={'identity_style':False}
    if route=='SessionAnalysis.Apply':config['analysis_decision_sha']='d'*64
    else:config['overrides_id']=str(uuid4())
    c.execute('UPDATE photo.runs SET config_snapshot=%s::jsonb WHERE id=%s',(json.dumps(config),run))
    key=key or str(run)
    c.execute("SELECT set_config('app.tenant_id',%s,true)",(str(t),))
    assert c.execute("SELECT photo.billing_prepare_apply(%s,%s,%s,%s)",(t,actor,session,route)).fetchone()==(True,)
    return c.execute('SELECT photo.billing_bind_routed_apply(%s,%s,%s,%s,%s,%s,%s,%s)',(t,actor,session,run,key,sha256(key.encode()).digest(),instance,route)).fetchone()[0]


@pytest.fixture(params=['SessionAnalysis.Apply','Run.SaveEdits'])
def route(request,monkeypatch):
    route=request.param
    monkeypatch.setattr(base,'bind',lambda *a,**kw:routed_bind(*a,**kw,route=route))
    return route

@pytest.mark.parametrize('scenario',[
 'test_final_failure_returns_once_and_cannot_resurrect_refunded_run',
 'test_cancellation_finalization_returns_funding_and_removes_initial_hold',
 'test_batch_known_deficit_is_atomic',
 'test_two_free_reruns_do_not_charge_and_failure_returns_slot',
 'test_adoption_refusal_rolls_back_partial_outputs_and_billing',
 'test_single_request_allowance_then_free_rerun_even_when_overrun',
 'test_free_slot_decision_is_pinned_even_if_another_request_releases_slot'])
def test_routed_terminal_and_negative_paths(db,route,scenario):
    getattr(base,scenario)(db)

def test_routed_canonical_success_and_child_not_double_billed(db,route):
    def admission(c,t,r,k,items,out):
        base.bind(c,t,r,k,items,out)
        actor,session=c.execute('SELECT actor_user_id,session_id FROM photo.billing_usage_requests WHERE run_id=%s',(r,)).fetchone()
        # Attempt duplicate child binding inside the actual admission window,
        # before canonical fixture tasks or graph sealing exist.
        rejects(c,'SELECT photo.billing_bind_apply(%s,%s,%s,%s,%s,%s,%s)',(t,actor,session,r,'child-second',sha256(b'child-second').digest(),'api-test'),'BILLING_ROUTE_INVALID' if route=='SessionAnalysis.Apply' else 'BILLING_IDEMPOTENCY_CONFLICT')
    t,r,k,items,out=_adoptable(db,4,admit=admission)
    assert db.execute('SELECT route,workflow_kind FROM photo.billing_usage_requests WHERE run_id=%s',(r,)).fetchone()==(route,'session_analysis_apply' if route=='SessionAnalysis.Apply' else 'save_edits_style')
    assert adopt(db,t,r,k)
    assert consumed(db,t)==(2,0)
    assert not adopt(db,t,r,k)
    assert db.execute('SELECT count(*) FROM photo.billing_usage_requests WHERE run_id=%s',(r,)).fetchone()==(1,)
    check_conservation(db,t)

@pytest.mark.parametrize('scenario',['test_concurrent_duplicate_adoption_never_double_spends','test_concurrent_adoption_vs_failure_is_atomic','test_concurrent_initial_requests_cannot_create_two_paid_first_results'])
def test_routed_concurrency(database,route,scenario):
    getattr(base,scenario)(database)

def test_routed_binding_replay_and_conflict(db,route):
    t,r,k,items,out=_adoptable(db,4,admit=base.bind)
    req,actor,session,key=db.execute('SELECT id,actor_user_id,session_id,request_key FROM photo.billing_usage_requests WHERE run_id=%s',(r,)).fetchone()
    args=(t,actor,session,r,key,sha256(key.encode()).digest(),'api-test',route)
    assert db.execute('SELECT photo.billing_bind_routed_apply(%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()==(req,)
    rejects(db,'SELECT photo.billing_bind_routed_apply(%s,%s,%s,%s,%s,%s,%s,%s)',(*args[:5],sha256(b'changed').digest(),*args[6:]),'BILLING_IDEMPOTENCY_CONFLICT')
    assert db.execute('SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==(2,)

@pytest.mark.parametrize("api_route",["SessionAnalysis.Apply","Run.SaveEdits"])
def test_real_go_routed_api_admission_replay_cancel_and_revocation(database,api_route):
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
    env=dict(os.environ,PHOTO_BILLING_ROUTE=api_route,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_BILLING_APPLY_FIXTURE=json.dumps(fixture))
    result=subprocess.run(['/usr/local/go/bin/go','test','./internal/run','-run','^TestIntegrationBillingRoutedAdmission$','-count=1','-v'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert '--- PASS: TestIntegrationBillingRoutedAdmission' in result.stdout
    with psycopg.connect(**database) as c:check_conservation(c,t)



def test_real_session_analysis_parent_commit_replay_refund_and_revocation(database):
    import os,subprocess
    from pathlib import Path
    from psycopg.conninfo import make_conninfo
    root=Path(__file__).resolve().parents[2]
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database))
    result=subprocess.run(['/usr/local/go/bin/go','test','./internal/v2sessions','-run','^TestIntegrationAnalysisApplyBilling$','-count=1','-v'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert '--- PASS: TestIntegrationAnalysisApplyBilling' in result.stdout
