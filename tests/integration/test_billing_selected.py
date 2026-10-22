"""Native selected/final authority + quota kernel, synthetic relational artifacts (no GPU)."""
from uuid import uuid4
import pytest,psycopg
from psycopg.types.json import Jsonb
from selected_local_fixture import adopted_local_result
from selected_open_eyes_fixture import adopted_open_eyes
from test_billing_apply import enroll
from test_billing_operations import policy,projection,settle
from test_billing_quota import grant,check_conservation,rejects,race
pytestmark=pytest.mark.migration_head(2609)

def admit(c,t,run,operation,quantity=8):
    enroll(c,t,0)
    c.execute('INSERT INTO photo.billing_operation_controls(tenant_id,enabled) VALUES(%s,true)',(t,))
    res=policy(c,operation=operation,context='preview')
    contract=c.execute("SELECT photo.billing_selected_contract(%s,'1')",(operation,)).fetchone()[0]
    c.execute('UPDATE photo.billing_operation_policies SET success_contract=%s WHERE resource_code=%s',(contract,res))
    if quantity:grant(c,t,quantity,resource=res)
    c.execute('SELECT photo.billing_bind_operations(%s,%s)',(t,run))

def local(c,operation='portrait.hair',review=False,quantity=8):
    item=adopted_local_result(c,operation,review_required=review,admit=lambda *a:admit(*a,quantity=quantity))
    c.execute('SET LOCAL ROLE photo_scheduler')
    value=c.execute('SELECT photo.publish_v28_local_portrait_result(%s,%s,%s)',(item['tenant'],item['task'],item['result'])).fetchone()[0]
    c.execute('RESET ROLE');assert value is not None
    item['resource']='operation.fixture_'+operation.replace('.','_')
    return item

def eyes(c):
    item=adopted_open_eyes(c,admit=admit)
    item['run'],item['task']=c.execute('SELECT run_id,task_id FROM photo.reviews WHERE id=%s',(item['review'],)).fetchone()
    item['resource']='operation.fixture_portrait_open_eyes'
    return item

def test_local_auto_exact_native_result_debits_once(db):
    item=local(db);t=item['tenant']
    assert db.execute('SELECT photo.billing_selected_outcome(%s,%s,%s,%s)',(t,item['run'],item['task'],'local_portrait_selected_v1')).fetchone() is not None
    assert settle(db);assert not settle(db)
    assert projection(db,t,item['resource'])==(1,0)
    assert db.execute('SELECT count(*) FROM photo.billing_operation_selections WHERE tenant_id=%s',(t,)).fetchone()==(1,)
    check_conservation(db,t)

def test_open_eyes_native_final_and_materialization_never_double_charge(db):
    item=eyes(db);t=item['tenant']
    assert not settle(db)  # waiting_review is not a financial terminal state
    assert db.execute('SELECT photo.v28_open_eyes_selected_run_ready(%s,%s)',(t,item['run'])).fetchone()==(True,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.finalize_v28_open_eyes_selected_run()').fetchone()==(True,)
    db.execute('RESET ROLE')
    assert settle(db);assert not settle(db)
    for _ in range(2):
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(t),))
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT photo.apply_v28_open_eyes_version(%s,%s,%s,%s,%s,%s,%s)',(t,item['review'],item['actor'],item['session'],'a'*64,item['base'],item['image'])).fetchone()[0] is not None
        db.execute('RESET ROLE')
    assert projection(db,t,item['resource'])==(1,0)
    assert db.execute('SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=%s',(t,)).fetchone()==(1,)
    assert db.execute('SELECT count(*) FROM photo.billing_operation_children WHERE tenant_id=%s',(t,)).fetchone()[0]>=1
    check_conservation(db,t)

def local_review(c,decision='accept'):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from local_portrait_preview_fixture import seed_review_media_inventory
    item=local(c,'portrait.clothing',review=True);t=item['tenant']
    receipt,snapshot,config=c.execute('SELECT v28_local_result_receipt,input_snapshot_id,config_snapshot FROM photo.runs WHERE id=%s',(item['run'],)).fetchone()
    import json
    base_version=json.loads(config['v28_local_portrait']['intent']['params_jcs'])['base']['asset_version_id']
    seed_review_media_inventory(c,item,receipt,snapshot,base_version)
    c.execute('RESET ROLE')
    item['review']=receipt['review_id']
    if decision!='pending':
        selection=dict(version='candidate_review_v1',candidate=receipt['candidate'],base=receipt['base'],revision=2)
        body=dict(version='candidate_decision_v1',actor_id=str(item['actor']),decision=decision,kind='portrait.clothing',review_id=str(item['review']),run_id=str(item['run']),selection=selection,tenant_id=str(t))
        digest=sha256(canonical_bytes(body)).hexdigest()
        c.execute("SELECT set_config('app.tenant_id',%s,true)",(str(t),))
        c.execute('SET LOCAL ROLE photo_api')
        assert c.execute('SELECT photo.decide_v28_local_portrait_review(%s,%s,%s,%s,1,%s,%s,%s,%s)',(t,item['review'],item['actor'],item['session'],decision,Jsonb(selection),digest,dict(accept='candidate',reject='base',skip='skip')[decision])).fetchone()==(True,)
        c.execute('RESET ROLE')
    return item

@pytest.mark.parametrize('decision',['accept','reject','skip','pending'])
def test_local_review_native_selection_and_refund(db,decision):
    item=local_review(db,decision);t=item['tenant']
    if decision=='pending':
        assert not settle(db);assert projection(db,t,item['resource'])==(0,0)
        assert db.execute('SELECT state FROM photo.billing_operation_items WHERE tenant_id=%s',(t,)).fetchone()==('accepted',)
    else:
        assert settle(db);assert not settle(db)
        assert db.execute('SELECT status FROM photo.runs WHERE id=%s',(item['run'],)).fetchone()==('completed',)
        assert projection(db,t,item['resource'])==((1,0) if decision=='accept' else (0,0))
        assert db.execute('SELECT route FROM photo.billing_operation_selections WHERE tenant_id=%s',(t,)).fetchone()==(dict(accept='candidate',reject='base',skip='skip')[decision],)
    check_conservation(db,t)

@pytest.mark.parametrize('damage',['candidate','root','qualification','session','adoption'])
def test_local_missing_live_final_proof_holds_then_recovers(db,damage):
    item=local(db);t=item['tenant']
    with db.transaction(force_rollback=True):
        if damage=='candidate':db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(item['candidate']['artifact_id'],))
        elif damage=='root':db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE created_by_task_id=%s AND output_role='document'",(item['task'],))
        elif damage=='qualification':db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(item['policy'],))
        elif damage=='session':db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(item['session'],))
        else:db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",(t,))
        assert not settle(db);assert projection(db,t,item['resource'])==(0,0)
        assert db.execute('SELECT state FROM photo.billing_operation_items WHERE tenant_id=%s',(t,)).fetchone()==('accepted',)
    assert settle(db);assert projection(db,t,item['resource'])==(1,0);check_conservation(db,t)

def test_open_eyes_consent_loss_never_becomes_refund(db):
    item=eyes(db);t=item['tenant']
    assert db.execute('SELECT photo.finalize_v28_open_eyes_selected_run()').fetchone()==(True,)
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.consent_grants SET status='revoked' WHERE id=%s",(item['consent'],))
        assert not settle(db);assert projection(db,t,item['resource'])==(0,0)
        assert db.execute('SELECT state FROM photo.billing_operation_items WHERE tenant_id=%s',(t,)).fetchone()==('accepted',)
    assert settle(db);assert projection(db,t,item['resource'])==(1,0)
    check_conservation(db,t)

@pytest.mark.parametrize('terminal',['failed','cancelled'])
def test_selected_failure_refunds_only_after_children_drain(db,terminal):
    from local_portrait_preview_fixture import seed_review_media_inventory
    import json
    item=local(db);t=item['tenant']
    receipt,snapshot,config=db.execute('SELECT v28_local_result_receipt,input_snapshot_id,config_snapshot FROM photo.runs WHERE id=%s',(item['run'],)).fetchone()
    version=json.loads(config['v28_local_portrait']['intent']['params_jcs'])['base']['asset_version_id']
    seed_review_media_inventory(db,item,receipt,snapshot,version);db.execute('RESET ROLE')
    children=db.execute('SELECT run_id FROM photo.billing_operation_children WHERE tenant_id=%s',(t,)).fetchall();assert children
    db.execute("UPDATE photo.runs SET status='running' WHERE id=%s",children[0])
    db.execute('UPDATE photo.runs SET status=%s,cancel_requested=%s WHERE id=%s',(terminal,terminal=='cancelled',item['run']))
    assert db.execute('SELECT photo.billing_settle_operations(%s,%s)',(t,item['run'])).fetchone()==(False,)
    db.execute("UPDATE photo.runs SET status='completed' WHERE id=%s",children[0])
    assert settle(db);assert not settle(db);assert projection(db,t,item['resource'])==(0,0)
    for r in [item['run'],children[0][0]]:rejects(db,"UPDATE photo.runs SET status='running' WHERE id=%s",(r,),'BILLING_RUN_TERMINAL')
    check_conservation(db,t)

@pytest.mark.parametrize('decision',['auto','accept','reject','skip'])
def test_selected_concurrent_completion_and_settlement(database,decision):
    with psycopg.connect(**database) as c:
        item=local(c) if decision=='auto' else local_review(c,decision)
    results=race(database,[lambda cx:settle(cx) for _ in range(6)])
    assert all(x[0]=='ok' for x in results),results
    assert sum(x[1] for x in results)==1,results
    with psycopg.connect(**database) as c:
        assert projection(c,item['tenant'],item['resource'])==((1,0) if decision in ['auto','accept'] else (0,0))
        check_conservation(c,item['tenant'])

@pytest.mark.parametrize('operation,contract',[
 ('portrait.glare','restoration_selected_v1'),('image.reflection','restoration_selected_v1'),
 ('portrait.open_eyes','open_eyes_selected_v1'),('image.relight','relight_selected_v1'),
 ('portrait.hair','local_portrait_selected_v1'),('portrait.clothing','local_portrait_selected_v1')])
def test_adapter_contract_is_exact_and_unbound_native_proof_is_unavailable(db,operation,contract):
    assert db.execute("SELECT photo.billing_selected_contract(%s,'1'),photo.billing_selected_contract(%s,'2')",(operation,operation)).fetchone()==(contract,None)
    assert db.execute('SELECT * FROM photo.billing_selected_outcome(%s,%s,%s,%s)',(uuid4(),uuid4(),uuid4(),contract)).fetchall()==[]
    for role in ['photo_api','photo_scheduler','photo_worker']:
        assert not db.execute("SELECT has_function_privilege(%s,'photo.billing_selected_outcome(uuid,uuid,uuid,text)','EXECUTE')",(role,)).fetchone()[0]

def test_specialized_operation_cannot_use_generic_policy(db):
    def bad(c,t,run,op):
        enroll(c,t,0);c.execute('INSERT INTO photo.billing_operation_controls(tenant_id,enabled) VALUES(%s,true)',(t,))
        policy(c,operation=op,context='preview')
        c.execute('SELECT photo.billing_bind_operations(%s,%s)',(t,run))
    with pytest.raises(psycopg.Error,match='BILLING_OPERATION_POLICY_REQUIRED'):
        with db.transaction():adopted_local_result(db,admit=bad)
    assert db.execute('SELECT count(*) FROM photo.billing_operation_requests').fetchone()==(0,)
