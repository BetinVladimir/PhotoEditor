"""Durable render funding, canonical metadata and real PostgreSQL races (no pixels/Stripe)."""
from uuid import uuid4,uuid5,UUID
from hashlib import sha256
import json
import pytest,psycopg
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition
from test_billing_apply import enroll,consumed
from test_billing_quota import check_conservation,race,rejects
from photo_worker.contracts.base import canonical_bytes
pytestmark=pytest.mark.migration_head(2607)

def seed(c,case=None,batch=True,copies=1,quantity=20,distinct=False):
    case=case or seed_original_composition(c)
    tenant,actor,session,project,snapshot=(case[k] for k in ('Tenant','Actor','Session','Project','Snapshot'))
    if not c.execute('SELECT 1 FROM photo.billing_render_controls WHERE tenant_id=%s',(tenant,)).fetchone():
        enroll(c,tenant,quantity)
        c.execute('INSERT INTO photo.billing_render_controls(tenant_id,enabled) VALUES(%s,true)',(tenant,))
    root=uuid4();children=[]
    binding=json.loads(case['Params'])['base']
    recipe=canonical_bytes(dict(scalars=dict(source=binding)));recipe_sha=sha256(recipe).hexdigest()
    params=c.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'test',%s,'v1',%s,%s,'application/json',now()) RETURNING id",(tenant,str(uuid4()),recipe_sha,len(recipe))).fetchone()[0]
    parts=[]
    if copies>1:
        snapshot=c.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,%s,now()+interval '1 hour') RETURNING id",(tenant,actor,'d'*64,copies)).fetchone()[0]
        snapshot=str(snapshot)
        case=dict(case,Snapshot=snapshot)
    for i in range(copies):
        child=uuid5(root,'render.batch:'+str(i)) if batch else root;workflow=uuid5(child,'workflow');task=uuid5(workflow,'render.compose:0')
        if distinct and i>0:
            source=binding['source']
            orig=c.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'test',%s,'v1',%s,64,'image/jpeg',now()) RETURNING id",(tenant,str(uuid4()),'e'*64)).fetchone()[0]
            asset=c.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,orig)).fetchone()[0]
            version=c.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tenant,asset,source['artifact_id'],source['sha256'])).fetchone()[0]
            binding=dict(binding,asset_version_id=str(version))
            recipe=canonical_bytes(dict(scalars=dict(source=binding)));recipe_sha=sha256(recipe).hexdigest()
            params=c.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'test',%s,'v1',%s,%s,'application/json',now()) RETURNING id",(tenant,str(uuid4()),recipe_sha,len(recipe))).fetchone()[0]
        if copies>1:
            c.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,%s,%s,%s,%s,'{}')",(tenant,snapshot,i,binding['asset_version_id'],binding['source']['artifact_id'],binding['source']['sha256']))
        node=dict(task_id=str(task),params=dict(artifact_id=str(params),sha256=recipe_sha),params_jcs=recipe.decode(),input_digest='b'*64)
        config=dict(project_id=project,input_snapshot_id=snapshot,v26_render=dict(auth_session_id=session,nodes=[node]));config_sha=sha256(canonical_bytes(config)).hexdigest()
        parts.append(dict(run_id=str(child),config_sha=config_sha,first_ordinal=i,frame_count=1,output_bytes=1))
        c.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,'fail_fast')",(child,tenant,snapshot,Jsonb(config),config_sha,actor))
        c.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',%s)",(workflow,tenant,project,child,snapshot,'a'*64))
        c.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'render.compose:0','render.compose','1','gpu','blocked','[]',%s,%s,'{\"renderer_version\":\"v2.2\"}')",(task,tenant,child,params,'b'*64))
        children.append((child,task,workflow))
    if copies>1:c.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))
    if batch:
        manifest=dict(schema_version=1,project_id=project,input_snapshot_id=snapshot,auth_session_id=session,policy_sha='a'*64,parts=parts)
        c.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,%s,'bulk','draft',%s,%s,%s,%s)",(root,tenant,snapshot,Jsonb(manifest),sha256(canonical_bytes(manifest)).hexdigest(),actor))
    return case,root,children

def bind(c,case,root,instance='render-test'):
    c.execute('SET LOCAL ROLE photo_scheduler')
    try:
        with c.transaction():return c.execute('SELECT photo.billing_bind_render(%s,%s,%s,%s,%s)',(case['Tenant'],root,case['Actor'],case['Session'],instance)).fetchone()[0]
    finally:c.execute('RESET ROLE')

def settle(c):
    c.execute('SET LOCAL ROLE photo_scheduler')
    try:
        with c.transaction():return c.execute('SELECT photo.billing_settle_next_render()').fetchone()[0]
    finally:c.execute('RESET ROLE')

def finish(c,case,root,children,outcome='success',review=None,finalize=True,terminal='completed'):
    tenant=case['Tenant']
    for child,task,workflow in children:
        if outcome in ('failed','cancelled'):
            c.execute('UPDATE photo.tasks SET state=%s,terminal_reason=%s WHERE id=%s',(outcome,'fixture',task))
            c.execute('UPDATE photo.runs SET status=%s,graph_sealed=true WHERE id=%s',(outcome,child));continue
        c.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cuda','{}')",(tenant,task,uuid4()))
        artifacts=[]
        for role in ('root','working_image'):
            a=c.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,64,'application/json','document',now(),%s,1,1) RETURNING id",(tenant,str(uuid4()),'a'*64,task)).fetchone()[0]
            artifacts.append(a)
            path='tasks/'+str(task)+'/$'+('/working_image' if role=='working_image' else '')
            c.execute('INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,%s,0)',(tenant,case['Project'],workflow,a,'a'*64,sha256(path.encode()).hexdigest()))
        c.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,'document',bucket,object_key,sha256,bytes,media_type,'ready',object_version,now() FROM photo.artifacts WHERE id=%s",(task,uuid4(),'a'*64,uuid4(),artifacts[0]))
        result=c.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest,review_required) VALUES(%s,%s,1,1,%s,'{}',%s,%s) RETURNING id",(tenant,task,Jsonb([dict(artifact_id=str(artifacts[0]),sha256='a'*64)]),'c'*64,review is not None)).fetchone()[0]
        c.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s WHERE id=%s",(result,task))
        c.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),now())",(tenant,task,result))
        if review is not None:
            source=json.loads(case['Params'])['base']['source']
            c.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,'WorkingImage',0)",(tenant,case['Project'],task,source['artifact_id'],source['sha256']))
            c.execute("UPDATE photo.runs SET status='waiting_review',graph_sealed=true WHERE id=%s",(child,))
            c.execute('SET LOCAL ROLE photo_scheduler')
            try:rid=c.execute('SELECT photo.create_v26_render_review(%s,%s,%s,%s,%s)',(tenant,task,artifacts[0],artifacts[1],source['artifact_id'])).fetchone()[0]
            finally:c.execute('RESET ROLE')
            assert rid is not None
            if review=='pending':continue
            selection=dict(version='candidate_review_v1',candidate=dict(artifact_id=str(artifacts[1]),sha256='a'*64),base=source,revision=2)
            c.execute("UPDATE photo.reviews SET state='decided',revision=2,decision=%s,decision_route=%s,decision_hash=%s,decided_by=%s,decided_at=now(),selection=%s WHERE id=%s",(review,dict(accept='candidate',reject='base',skip='skip')[review],'f'*64,case['Actor'],Jsonb(selection),rid))
            assert c.execute('SELECT route FROM photo.v26_render_review_selection(%s,%s)',(tenant,task)).fetchone()==(dict(accept='candidate',reject='base',skip='skip')[review],)
        c.execute("UPDATE photo.runs SET status=%s,graph_sealed=true WHERE id=%s",(terminal,child))
    if review=='pending':
        c.execute("UPDATE photo.runs SET status='waiting_review' WHERE id=%s",(root,));return
    if finalize and root not in [x[0] for x in children]:
        c.execute('SET LOCAL ROLE photo_scheduler')
        try:assert c.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(True,)
        finally:c.execute('RESET ROLE')

def test_render_single_debit_for_multiple_children_and_replay(db):
    case,root,children=seed(db,copies=2);req=bind(db,case,root)
    assert bind(db,case,root)==req
    assert consumed(db,case['Tenant'])==(0,0)
    assert db.execute('SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(1,)
    rejects(db,'SELECT photo.billing_bind_render(%s,%s,%s,%s,%s)',(case['Tenant'],children[0][0],case['Actor'],case['Session'],'other'),'BILLING_CHILD_NOT_ROOT')
    assert not settle(db)
    finish(db,case,root,children);assert settle(db);assert not settle(db)
    assert consumed(db,case['Tenant'])==(1,0)
    assert db.execute('SELECT count(*) FROM photo.billing_render_results WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(1,)
    check_conservation(db,case['Tenant'])

@pytest.mark.parametrize('batch',[False,True])
@pytest.mark.parametrize('outcome',['success','failed','cancelled'])
def test_render_settlement_and_refund(db,batch,outcome):
    case,root,children=seed(db,batch=batch);bind(db,case,root);finish(db,case,root,children,outcome)
    assert settle(db);assert not settle(db)
    assert consumed(db,case['Tenant'])==((1,0) if outcome=='success' else (0,0))
    expected='settled' if outcome=='success' else 'released'
    assert db.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(expected,)
    rejects(db,"UPDATE photo.runs SET status='running' WHERE id=%s",(children[0][0],),'BILLING_RUN_TERMINAL',psycopg.errors.CheckViolation)
    check_conservation(db,case['Tenant'])

def test_render_free_reruns_and_failure_releases_slot(db):
    case,root,ch=seed(db);bind(db,case,root);finish(db,case,root,ch);assert settle(db)
    for outcome in ['failed','success','success','success']:
        _,r,ch=seed(db,case);bind(db,case,r);finish(db,case,r,ch,outcome);assert settle(db)
    assert consumed(db,case['Tenant'])==(2,0)
    check_conservation(db,case['Tenant'])

def test_render_missing_adoption_holds_funding(db):
    case,root,ch=seed(db);bind(db,case,root);finish(db,case,root,ch)
    db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",(case['Tenant'],))
    assert not settle(db)
    assert db.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==('accepted',)
    assert consumed(db,case['Tenant'])==(0,0)

def test_render_authority_sealing_and_roles(db):
    case,root,ch=seed(db)
    rejects(db,'SELECT photo.billing_bind_render(%s,%s,%s,%s,%s)',(case['Tenant'],root,case['Actor'],uuid4(),'test'),'BILLING_AUTHORITY_INVALID')
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.seal_v2_graph(%s,%s)',(case['Tenant'],ch[0][0])).fetchone()==(False,)
    db.execute('RESET ROLE')
    for role in ['photo_api','photo_worker','public']:
        assert not db.execute("SELECT has_function_privilege(%s,'photo.billing_bind_render(uuid,uuid,uuid,uuid,text)','EXECUTE')",(role,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_scheduler','photo.billing_settle_render(uuid,uuid)','EXECUTE')").fetchone()[0]

@pytest.mark.parametrize('action',['settle','bind'])
def test_render_concurrent_exact_replays(database,action):
    with psycopg.connect(**database,autocommit=True) as c:
        with c.transaction():
            case,root,ch=seed(c);req=bind(c,case,root)
            if action=='settle':finish(c,case,root,ch)
        result=race(database,[lambda cx: settle(cx) if action=='settle' else bind(cx,case,root) for _ in range(6)])
        assert all(x[0]=='ok' for x in result),result
        if action=='settle':assert sum(x[1] for x in result)==1
        else:assert all(x[1]==req for x in result)
        check_conservation(c,case['Tenant'])

@pytest.mark.parametrize('review',['pending','accept','reject','skip'])
def test_render_review_selection_controls_billing(db,review):
    case,root,ch=seed(db);bind(db,case,root);finish(db,case,root,ch,review=review)
    assert settle(db)==(review!='pending')
    assert consumed(db,case['Tenant'])==((1,0) if review=='accept' else (0,0))
    expected='accepted' if review=='pending' else ('settled' if review=='accept' else 'released')
    assert db.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(expected,)
    check_conservation(db,case['Tenant'])

def test_render_allowance_is_finite_and_free_reruns_do_not_expand_it(db):
    case,root,ch=seed(db,quantity=0);bind(db,case,root);finish(db,case,root,ch);assert settle(db)
    assert consumed(db,case['Tenant'])==(1,1)
    for _ in range(2):
        _,r,ch=seed(db,case);bind(db,case,r);finish(db,case,r,ch);assert settle(db)
    with pytest.raises(psycopg.Error,match='BILLING_OVERRUN_BLOCKED'):
        with db.transaction():
            _,r,ch=seed(db,case);bind(db,case,r)
    assert consumed(db,case['Tenant'])==(1,1)
    check_conservation(db,case['Tenant'])

@pytest.mark.parametrize('mutation',['expiry','fence','missing_image'])
def test_render_uncertain_proof_never_refunds_and_does_not_starve_other_tenant(db,mutation):
    case,root,ch=seed(db);bind(db,case,root);finish(db,case,root,ch)
    if mutation=='expiry':db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE created_by_task_id=%s",(ch[0][1],))
    elif mutation=='fence':db.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(ch[0][1],))
    else:db.execute("DELETE FROM photo.v2_artifact_links WHERE tenant_id=%s AND purpose=%s",(case['Tenant'],sha256(('tasks/'+str(ch[0][1])+'/$/working_image').encode()).hexdigest()))
    other,r,ch2=seed(db);bind(db,other,r);finish(db,other,r,ch2)
    assert settle(db)
    assert consumed(db,case['Tenant'])==(0,0)
    assert consumed(db,other['Tenant'])==(1,0)
    assert db.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==('accepted',)
    check_conservation(db,case['Tenant']);check_conservation(db,other['Tenant'])

def test_render_finalization_cancel_and_settlement_race(database):
    with psycopg.connect(**database,autocommit=True) as c:
        with c.transaction():
            case,root,ch=seed(c);bind(c,case,root);finish(c,case,root,ch,'failed',finalize=False)
        def finalize(cx):return cx.execute('SELECT photo.finalize_v26_render_batch()').fetchone()[0]
        def cancel(cx):return cx.execute('SELECT photo.cancel_v26_render_batch(%s,%s,%s,%s)',(case['Tenant'],root,case['Actor'],case['Session'])).fetchone()[0]
        results=race(database,[finalize,cancel,settle])
        assert all(x[0]=='ok' for x in results),results
        with c.transaction():
            c.execute('SELECT photo.finalize_run_cancellation()')
            settle(c)
        assert c.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==('released',)
        assert consumed(c,case['Tenant'])==(0,0)
        check_conservation(c,case['Tenant'])


def test_render_known_batch_deficit_is_atomic(db):
    case,root,ch=seed(db,copies=2,quantity=1,distinct=True)
    rejects(db,'SELECT photo.billing_bind_render(%s,%s,%s,%s,%s)',(case['Tenant'],root,case['Actor'],case['Session'],'test'),'BILLING_QUOTA_EXCEEDED')
    assert db.execute('SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(0,)
    assert db.execute('SELECT count(*) FROM photo.billing_funding_claims WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(0,)
    check_conservation(db,case['Tenant'])

def test_render_concurrent_independent_initials_have_one_winner(database):
    with psycopg.connect(**database,autocommit=True) as c:
        with c.transaction():
            case,first,ch=seed(c);_,second,ch2=seed(c,case)
        results=race(database,[lambda cx:bind(cx,case,first),lambda cx:bind(cx,case,second)])
        assert sum(x[0]=='ok' for x in results)==1,results
        assert sum(x[0]=='error' and 'PHOTO_INITIAL_IN_PROGRESS' in x[2] for x in results)==1,results
        assert c.execute('SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(1,)
        check_conservation(c,case['Tenant'])

def test_render_mixed_batch_only_charges_visible_success(db):
    case,root,ch=seed(db,copies=2,distinct=True);bind(db,case,root)
    finish(db,case,root,ch[:1],finalize=False,terminal='failed')
    finish(db,case,root,ch[1:],'failed')
    assert settle(db)
    assert consumed(db,case['Tenant'])==(1,0)
    assert db.execute('SELECT state,count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s GROUP BY state ORDER BY state',(case['Tenant'],)).fetchall()==[('released',1),('settled',1)]
    check_conservation(db,case['Tenant'])

def test_render_reconciliation_recovers_without_rebinding_or_new_ticket(db):
    case,root,ch=seed(db);bind(db,case,root);finish(db,case,root,ch)
    db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",(case['Tenant'],))
    assert not settle(db)
    task=ch[0][1]
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) SELECT tenant_id,'v2-adopt',photo.v2_adoption_event_id(id,canonical_result_id),now() FROM photo.tasks WHERE id=%s",(task,))
    db.execute("UPDATE photo.billing_usage_requests SET render_reconcile_after=clock_timestamp()-interval '1 second' WHERE run_id=%s",(root,))
    assert settle(db);assert not settle(db)
    assert db.execute('SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(1,)
    assert consumed(db,case['Tenant'])==(1,0)
    check_conservation(db,case['Tenant'])

def test_render_restart_reclaims_unissued_funding_only(db):
    case,root,ch=seed(db);bind(db,case,root);finish(db,case,root,ch);assert settle(db)
    for _ in range(2):
        _,root,ch=seed(db,case);bind(db,case,root);finish(db,case,root,ch);assert settle(db)
    old=db.execute("SELECT id FROM photo.billing_admission_leases WHERE tenant_id=%s AND instance_id='render-test'",(case['Tenant'],)).fetchone()[0]
    db.execute("UPDATE photo.billing_admission_leases SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(old,))
    _,root,ch=seed(db,case);bind(db,case,root,instance='render-restarted');finish(db,case,root,ch);assert settle(db)
    assert db.execute('SELECT state,returned_quantity FROM photo.billing_admission_leases WHERE id=%s',(old,)).fetchone()==('closed',4)
    assert consumed(db,case['Tenant'])==(2,0)
    assert db.execute('SELECT count(*),max(settled_quantity) FROM photo.billing_allowance_budgets WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(1,0)
    check_conservation(db,case['Tenant'])


def test_render_delayed_settlement_does_not_restart_free_window(db):
    case,root,ch=seed(db);bind(db,case,root);finish(db,case,root,ch)
    task=ch[0][1]
    db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",(case['Tenant'],))
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) SELECT tenant_id,'v2-adopt',photo.v2_adoption_event_id(id,canonical_result_id),clock_timestamp()-interval '170 hours' FROM photo.tasks WHERE id=%s",(task,))
    assert settle(db)
    assert db.execute('SELECT window_ends_at<clock_timestamp() FROM photo.billing_photo_identities WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(True,)
    _,r,ch=seed(db,case);bind(db,case,r);finish(db,case,r,ch);assert settle(db)
    assert consumed(db,case['Tenant'])==(2,0)
    check_conservation(db,case['Tenant'])
