"""Real PostgreSQL operation accounting; synthetic policy/metadata, no production prices or pixels."""
from uuid import uuid4,uuid5
from hashlib import sha256
import pytest,psycopg
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition
from test_billing_apply import enroll
from test_billing_quota import grant,check_conservation,race,rejects
pytestmark=pytest.mark.migration_head(2607)

def policy(c,operation='image.color',context='standalone',beta=False,allowance=1):
    resource=('operation.beta.fixture_' if beta else 'operation.fixture_')+operation.replace('.','_')
    c.execute("INSERT INTO photo.billing_resource_types(code,unit,kind,value_type,description) VALUES(%s,'credit','consumable','integer','synthetic operation qualification') ON CONFLICT DO NOTHING",(resource,))
    c.execute("INSERT INTO photo.billing_admission_policies(resource_code,policy_version,chunk_quantity,lease_seconds,cache_ttl_seconds,optimistic_allowance,max_local_leases,cold_timeout_ms) VALUES(%s,1,8,60,30,%s,16,200) ON CONFLICT DO NOTHING",(resource,allowance))
    c.execute("INSERT INTO photo.billing_operation_policies(operation_code,operation_version,charge_context,policy_version,billing_class,resource_code,units_per_success,success_contract,rerun_policy,beta_budget,effective_from,benchmark_ref,policy_snapshot_hash) VALUES(%s,'1',%s,1,%s,%s,%s,'v2_adopted_outputs_v1','none',%s,now()-interval '1 hour','SYNTHETIC TEST ONLY',%s)",(operation,context,'free_beta' if beta else 'paid_operation',resource,0 if beta else 1,2 if beta else None,sha256(b'fixture-policy').digest()))
    return resource

def inventory(c,count=1,quantity=8,beta=False,enabled=True,install=True):
    case=seed_original_composition(c);t=case['Tenant'];enroll(c,t,0)
    c.execute('INSERT INTO photo.billing_operation_controls(tenant_id,enabled) VALUES(%s,%s)',(t,enabled))
    resource=policy(c,beta=beta) if install else None
    if quantity and resource:grant(c,t,quantity,resource=resource)
    run,tasks=next_run(c,case,count)
    return case,run,tasks,resource

def next_run(c,case,count=1):
    t=case['Tenant'];run=uuid4();workflow=uuid5(run,'workflow')
    snapshot=case['Snapshot']
    if count>1:
        snapshot=c.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,%s,now()+interval '1 hour') RETURNING id",(t,case['Actor'],'e'*64,count)).fetchone()[0]
        base=c.execute('SELECT asset_version_id,artifact_id,sha256 FROM photo.snapshot_items WHERE snapshot_id=%s',(case['Snapshot'],)).fetchone()
        for i in range(count):
            v=base[0]
            if i:
                orig=c.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'fixture',%s,'v1',%s,64,'image/jpeg',now()) RETURNING id",(t,str(uuid4()),sha256(str(uuid4()).encode()).hexdigest())).fetchone()[0]
                a=c.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(t,case['Project'],orig)).fetchone()[0]
                v=c.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(t,a,base[1],base[2])).fetchone()[0]
            c.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,%s,%s,%s,%s,'{}')",(t,snapshot,i,v,base[1],base[2]))
        c.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))

    c.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,'fail_fast')",(run,t,snapshot,Jsonb(dict(project_id=case['Project'],auth_session_id=case['Session'])),'a'*64,case['Actor']))
    c.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(workflow,t,case['Project'],run,snapshot,'b'*64))
    tasks=[]
    for i in range(count):
        task=uuid4();tasks.append(task)
        c.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,%s,'image.color','1','cpu','blocked','[]',%s,%s,'{\"renderer_version\":\"v2.2\"}')",(task,t,run,'image.color:'+str(i),case['Artifact']['artifact_id'],sha256(str(i).encode()).hexdigest()))
    return run,tasks

def bind(c,t,run):return c.execute('SELECT photo.billing_bind_operations(%s,%s)',(t,run)).fetchone()[0]
def settle(c):
    with c.transaction():
        c.execute('SET LOCAL ROLE photo_scheduler')
        result=c.execute('SELECT photo.billing_settle_next_operations()').fetchone()[0]
    c.execute('RESET ROLE');return result

def finish(c,case,run,tasks,outcome='success',review=False):
    t=case['Tenant']
    for task in tasks:
        if outcome!='success':
            c.execute("UPDATE photo.tasks SET state=%s,terminal_reason='fixture' WHERE id=%s",(outcome,task));continue
        c.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cpu','{}')",(t,task,uuid4()))
        artifact=c.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'fixture',%s,'v1',%s,64,'application/json','document',now(),%s,1,1) RETURNING id",(t,str(uuid4()),'a'*64,task)).fetchone()[0]
        c.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,'document',bucket,object_key,sha256,bytes,media_type,'ready',object_version,now() FROM photo.artifacts WHERE id=%s",(task,uuid4(),'a'*64,uuid4(),artifact))
        result=c.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest,review_required) VALUES(%s,%s,1,1,%s,'{}',%s,%s) RETURNING id",(t,task,Jsonb([dict(artifact_id=str(artifact),sha256='a'*64)]),'b'*64,review)).fetchone()[0]
        c.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s WHERE id=%s",(result,task))
        c.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),now())",(t,task,result))
        c.execute("INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) SELECT %s,%s,id,%s,%s,%s,0 FROM photo.v2_workflows WHERE run_id=%s",(t,case['Project'],artifact,'a'*64,sha256(('tasks/'+str(task)+'/$').encode()).hexdigest(),run))
    c.execute("UPDATE photo.runs SET status=%s,graph_sealed=true WHERE id=%s",('completed' if outcome=='success' else outcome,run))

def projection(c,t,res):return c.execute('SELECT consumed,overrun FROM photo.billing_project_quota(%s,%s)',(t,res)).fetchone()

@pytest.mark.parametrize("count",[1,2,10])
def test_operation_success_replay_and_isolation(db,count):
    case,run,tasks,res=inventory(db,count=count,quantity=max(8,count+2));t=case['Tenant'];req=bind(db,t,run)
    assert bind(db,t,run)==req;assert projection(db,t,res)==(0,0)
    assert db.execute('SELECT count(*) FROM photo.billing_admission_leases WHERE tenant_id=%s AND resource_code=%s',(t,res)).fetchone()==((count+7)//8,)
    finish(db,case,run,tasks);assert settle(db);assert not settle(db)
    assert projection(db,t,res)==(count,0)
    assert db.execute("SELECT count(*) FROM photo.billing_resource_ledger WHERE tenant_id=%s AND resource_code='standard_photo'",(t,)).fetchone()==(0,)
    check_conservation(db,t)

@pytest.mark.parametrize('outcome',['failed','cancelled'])
def test_operation_failure_refund_once(db,outcome):
    case,run,tasks,res=inventory(db);t=case['Tenant'];bind(db,t,run)
    db.execute("UPDATE photo.runs SET status=%s WHERE id=%s",(outcome,run))
    assert db.execute('SELECT photo.billing_settle_operations(%s,%s)',(t,run)).fetchone()==(False,)
    finish(db,case,run,tasks,outcome);assert settle(db);assert not settle(db)
    assert projection(db,t,res)==(0,0);check_conservation(db,t)
    rejects(db,"UPDATE photo.runs SET status='running' WHERE id=%s",(run,),'BILLING_RUN_TERMINAL')

@pytest.mark.parametrize('damage',['adoption','unverified','expired','links','fence'])
def test_operation_missing_proof_holds(db,damage):
    case,run,tasks,res=inventory(db);t=case['Tenant'];bind(db,t,run);finish(db,case,run,tasks)
    if damage=='adoption':db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",(t,))
    elif damage=='links':db.execute('DELETE FROM photo.v2_artifact_links WHERE tenant_id=%s',(t,))
    elif damage=='fence':db.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(tasks[0],))
    elif damage=='unverified':db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE created_by_task_id=%s',(tasks[0],))
    else:db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE created_by_task_id=%s",(tasks[0],))
    assert not settle(db)
    assert db.execute('SELECT state FROM photo.billing_operation_items WHERE tenant_id=%s',(t,)).fetchone()==('accepted',)
    assert projection(db,t,res)==(0,0);check_conservation(db,t)

def test_operation_unknown_policy_and_preview_context_denied(db):
    case,run,tasks,res=inventory(db,install=False);t=case['Tenant']
    rejects(db,'SELECT photo.billing_bind_operations(%s,%s)',(t,run),'BILLING_OPERATION_POLICY_REQUIRED')
    policy(db,context='preview')
    rejects(db,'SELECT photo.billing_bind_operations(%s,%s)',(t,run),'BILLING_OPERATION_POLICY_REQUIRED')
    assert db.execute('SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=%s',(t,)).fetchone()==(0,)

def test_operation_finite_allowance_and_new_request_repetition(db):
    case,run,tasks,res=inventory(db,quantity=0);t=case['Tenant'];bind(db,t,run);finish(db,case,run,tasks);assert settle(db)
    assert projection(db,t,res)==(1,1)
    run2,_=next_run(db,case)
    rejects(db,'SELECT photo.billing_bind_operations(%s,%s)',(t,run2),'BILLING_OVERRUN_BLOCKED')
    check_conservation(db,t)

def test_operation_known_batch_deficit_atomic(db):
    case,run,tasks,res=inventory(db,count=2,quantity=1);t=case['Tenant']
    rejects(db,'SELECT photo.billing_bind_operations(%s,%s)',(t,run),'BILLING_QUOTA_EXCEEDED')
    assert db.execute('SELECT count(*) FROM photo.billing_operation_items WHERE tenant_id=%s',(t,)).fetchone()==(0,)
    check_conservation(db,t)

def test_operation_beta_budget_is_finite(db):
    case,run,tasks,res=inventory(db,beta=True);t=case['Tenant']
    bind(db,t,run)
    run2,_=next_run(db,case);bind(db,t,run2)
    run3,_=next_run(db,case)
    rejects(db,'SELECT photo.billing_bind_operations(%s,%s)',(t,run3),'BILLING_BETA_BUDGET_EXCEEDED')
    check_conservation(db,t)

@pytest.mark.parametrize('action',['bind','settle','refund'])
def test_operation_concurrent_replay(database,action):
    with psycopg.connect(**database) as c:
        case,run,tasks,res=inventory(c);t=case['Tenant']
        if action!='bind':bind(c,t,run);finish(c,case,run,tasks,'failed' if action=='refund' else 'success')
    results=race(database,[lambda cx:settle(cx) if action!='bind' else bind(cx,t,run) for _ in range(6)])
    assert all(x[0]=='ok' for x in results),results
    if action=='bind':assert len({x[1] for x in results})==1
    else:assert sum(x[1] for x in results)==1
    with psycopg.connect(**database) as c:check_conservation(c,t)

def test_operation_child_has_no_second_charge_and_parent_waits(db):
    case,run,tasks,res=inventory(db);t=case['Tenant'];bind(db,t,run)
    child,child_tasks=next_run(db,case)
    db.execute('SELECT photo.billing_link_operation_child(%s,%s,%s)',(t,run,child))
    assert bind(db,t,child) is None
    finish(db,case,run,tasks);assert not settle(db)
    finish(db,case,child,child_tasks)
    # Previous hold has a bounded scanner backoff; private retry uses durable proof.
    assert db.execute('SELECT photo.billing_settle_operations(%s,%s)',(t,run)).fetchone()==(True,)
    assert projection(db,t,res)==(1,0)
    assert db.execute('SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=%s',(t,)).fetchone()==(1,)
    check_conservation(db,t)

def test_operation_standard_editor_final_child_is_included(db):
    from test_billing_editor import inventory as editor_inventory
    case=editor_inventory(db);t,root,final=case[:3]
    db.execute('INSERT INTO photo.billing_operation_controls(tenant_id,enabled) VALUES(%s,true)',(t,))
    assert bind(db,t,final) is None
    assert db.execute('SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=%s',(t,)).fetchone()==(0,)

def test_operation_seal_refusal_rolls_back_accounting(db):
    case,run,tasks,res=inventory(db);t=case['Tenant']
    # Raw tenant fixture bypasses normal provisioning (which creates tenant_usage).
    # Preserve the native domain fence and prove its exception rolls back funding.
    tables=('billing_operation_requests','billing_operation_items','billing_admission_leases',
            'billing_funding_claims','billing_funding_claim_allocations',
            'billing_credit_buckets','billing_resource_ledger')
    def accounting_snapshot():
        from psycopg import sql
        return {name:db.execute(sql.SQL('SELECT to_jsonb(x) FROM photo.{} x WHERE tenant_id=%s ORDER BY id').format(sql.Identifier(name)),(t,)).fetchall() for name in tables}
    before=accounting_snapshot()
    if db.execute("SELECT to_regprocedure('photo.authored_mask_lock_execution_parent(uuid)') IS NOT NULL").fetchone()[0]:
        assert db.execute('SELECT count(*) FROM photo.tenant_usage WHERE tenant_id=%s',(t,)).fetchone()==(0,)
        with pytest.raises(psycopg.errors.SerializationFailure) as failure:
            with db.transaction():
                db.execute('SELECT photo.seal_v2_graph(%s,%s)',(t,run)).fetchone()
        assert failure.value.sqlstate=='40001'
        assert failure.value.diag.message_primary=='AUTHORED_MASK_EXECUTION_DOMAIN_CHANGED'
        assert accounting_snapshot()==before
        check_conservation(db,t)
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING',(t,))
    # With the native domain present, the legacy graph still refuses this closure.
    # No trigger or serialization guard is disabled.
    assert db.execute('SELECT photo.seal_v2_graph(%s,%s)',(t,run)).fetchone()==(False,)
    assert accounting_snapshot()==before
    assert db.execute('SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=%s',(t,)).fetchone()==(0,)
    assert projection(db,t,res)==(0,0);check_conservation(db,t)

def test_operation_control_disabled_and_private_aliases(db):
    case,run,_,res=inventory(db,enabled=False);t=case['Tenant']
    assert bind(db,t,run) is None
    for signature in ['billing_bind_operations(uuid,uuid)','billing_settle_operations(uuid,uuid)','billing_link_operation_child(uuid,uuid,uuid)','seal_before_operation_billing(uuid,uuid)']:
        for role in ['photo_api','photo_worker','photo_scheduler']:
            assert not db.execute("SELECT has_function_privilege(%s,'photo.'||%s,'EXECUTE')",(role,signature)).fetchone()[0]

def test_operation_batch_partial_success(db):
    case,run,tasks,res=inventory(db,count=2);t=case['Tenant'];bind(db,t,run)
    finish(db,case,run,tasks[:1]);finish(db,case,run,tasks[1:],'failed')
    assert settle(db);assert projection(db,t,res)==(1,0);check_conservation(db,t)

def test_operation_multi_resource_deficit_rolls_back_all(db):
    case,run,tasks,res=inventory(db,count=2);t=case['Tenant']
    other=policy(db,operation='quality.check')
    db.execute("UPDATE photo.tasks SET operation='quality.check',node_key='quality.check:1' WHERE id=%s",(tasks[1],))
    rejects(db,'SELECT photo.billing_bind_operations(%s,%s)',(t,run),'BILLING_QUOTA_EXCEEDED')
    assert db.execute('SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=%s',(t,)).fetchone()==(0,)
    assert db.execute('SELECT count(*) FROM photo.billing_funding_claims WHERE tenant_id=%s',(t,)).fetchone()==(0,)
    check_conservation(db,t)

def test_operation_independent_concurrent_requests_bound_allowance(database):
    with psycopg.connect(**database) as c:
        case,run,tasks,res=inventory(c,quantity=1);t=case['Tenant']
        roots=[run]+[next_run(c,case)[0] for _ in range(5)]
    results=race(database,[lambda cx,r=r:bind(cx,t,r) for r in roots])
    assert sum(x[0]=='ok' for x in results)==2,results
    assert all(x[0]=='ok' or 'BILLING_QUOTA_EXCEEDED' in x[2] for x in results),results
    with psycopg.connect(**database) as c:check_conservation(c,t)

def test_operation_real_go_cold_admission_and_seal_rollback(database):
    import os,json,subprocess
    from pathlib import Path
    from psycopg.conninfo import make_conninfo
    with psycopg.connect(**database) as c:
        case,run,*_=inventory(c)
        epoch=c.execute('SELECT auth_epoch FROM photo.users WHERE id=%s',(case['Actor'],)).fetchone()[0]
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_BILLING_OPERATION_FIXTURE=json.dumps(dict(Tenant=case['Tenant'],Run=str(run),Actor=case['Actor'],Session=case['Session'],Epoch=epoch)))
    result=subprocess.run(['/usr/local/go/bin/go','test','./internal/billing','-run','^TestIntegrationOperationColdAdmission$','-count=1','-v'],cwd=Path(__file__).resolve().parents[2]/'backend',env=env,capture_output=True,text=True,timeout=90)
    assert result.returncode==0,result.stdout+result.stderr
    assert '--- PASS: TestIntegrationOperationColdAdmission' in result.stdout


@pytest.mark.parametrize('decision',['pending','accept','reject','skip','wrong_candidate'])
def test_operation_review_matches_exact_canonical_candidate(db,decision):
    case,run,tasks,res=inventory(db);t=case['Tenant'];bind(db,t,run);finish(db,case,run,tasks,review=True)
    artifact=db.execute("SELECT outputs->0->>'artifact_id' FROM photo.task_results WHERE task_id=%s",(tasks[0],)).fetchone()[0]
    if decision=='wrong_candidate':artifact=case['Artifact']['artifact_id']
    digest=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()[0]
    review=db.execute("INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,candidate_hash,kind,reasons) VALUES(%s,%s,%s,%s,%s,'defects','[]') RETURNING id",(t,run,tasks[0],artifact,digest)).fetchone()[0]
    if decision!='pending':
        value='accept' if decision=='wrong_candidate' else decision
        db.execute("UPDATE photo.reviews SET state='decided',revision=2,decision=%s,decision_hash=%s,decision_route=%s,decided_by=%s,decided_at=now(),selection='{}' WHERE id=%s",(value,'e'*64,dict(accept='candidate',reject='base',skip='skip')[value],case['Actor'],review))
    if decision in ['pending','wrong_candidate']:
        assert not settle(db);assert projection(db,t,res)==(0,0)
    else:
        assert settle(db);assert projection(db,t,res)==((1,0) if decision=='accept' else (0,0))
    check_conservation(db,t)

def test_operation_cost_pin_and_immutable_item(db):
    case,run,tasks,res=inventory(db);t=case['Tenant'];bind(db,t,run)
    rejects(db,'UPDATE photo.billing_operation_items SET quantity=2 WHERE tenant_id=%s',(t,),'BILLING_OPERATION_ITEM_IMMUTABLE')
    db.execute("UPDATE photo.billing_operation_policies SET units_per_success=5 WHERE resource_code=%s",(res,))
    finish(db,case,run,tasks);assert settle(db);assert projection(db,t,res)==(1,0)
    check_conservation(db,t)


def test_operation_missing_proof_does_not_starve_other_requests_and_recovers(db):
    case,first,first_tasks,res=inventory(db);t=case['Tenant'];bind(db,t,first);finish(db,case,first,first_tasks)
    second,second_tasks=next_run(db,case);bind(db,t,second);finish(db,case,second,second_tasks)
    db.execute('DELETE FROM photo.v2_artifact_links WHERE tenant_id=%s AND workflow_id=(SELECT id FROM photo.v2_workflows WHERE run_id=%s)',(t,first))
    assert settle(db)
    assert db.execute('SELECT state FROM photo.billing_operation_requests WHERE run_id=%s',(first,)).fetchone()==('accepted',)
    assert db.execute('SELECT state FROM photo.billing_operation_requests WHERE run_id=%s',(second,)).fetchone()==('settled',)
    assert projection(db,t,res)==(1,0)
    task=first_tasks[0]
    artifact=db.execute("SELECT outputs->0->>'artifact_id' FROM photo.task_results WHERE task_id=%s",(task,)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) SELECT %s,%s,id,%s,%s,%s,0 FROM photo.v2_workflows WHERE run_id=%s",(t,case['Project'],artifact,'a'*64,sha256(('tasks/'+str(task)+'/$').encode()).hexdigest(),first))
    assert db.execute('SELECT photo.billing_settle_operations(%s,%s)',(t,first)).fetchone()==(True,)
    assert projection(db,t,res)==(2,0);check_conservation(db,t)

def test_operation_authority_rechecked_after_observed_quota_lock_wait(database):
    from concurrent.futures import ThreadPoolExecutor
    from queue import Queue
    import time
    with psycopg.connect(**database) as c:
        case,run,tasks,res=inventory(c);t=case['Tenant']
    pids=Queue()
    def blocked_bind():
        with psycopg.connect(**database) as c:
            pids.put(c.info.backend_pid)
            try:
                with c.transaction():return ('ok',bind(c,t,run))
            except psycopg.Error as e:return ('error',str(e).splitlines()[0])
    with psycopg.connect(**database) as holder,psycopg.connect(**database,autocommit=True) as observer:
        holder.execute('SELECT resource_code FROM photo.billing_resource_balances WHERE tenant_id=%s AND resource_code=%s FOR UPDATE',(t,res))
        with ThreadPoolExecutor(max_workers=1) as pool:
            future=pool.submit(blocked_bind);pid=pids.get(timeout=3);waiting=False
            try:
                for _ in range(200):
                    if observer.execute('SELECT cardinality(pg_blocking_pids(%s))>0',(pid,)).fetchone()[0]:waiting=True;break
                    time.sleep(.01)
                assert waiting,'admission must actually wait on the quota mutex'
                observer.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
            finally:holder.commit()
            result=future.result(timeout=10)
            assert result[0]=='error' and 'BILLING_AUTHORITY_INVALID' in result[1],result
        assert observer.execute('SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=%s',(t,)).fetchone()==(0,)
        assert observer.execute('SELECT count(*) FROM photo.billing_funding_claims WHERE tenant_id=%s',(t,)).fetchone()==(0,)
        check_conservation(observer,t)

@pytest.mark.parametrize('outcome',['success','failed'])
def test_paid_operation_above_chunk_is_exact_single_claim_and_settles_once(db,outcome):
    case,run,tasks,res=inventory(db,quantity=100);t=case['Tenant']
    db.execute('UPDATE photo.billing_operation_policies SET units_per_success=9 WHERE resource_code=%s',(res,))
    req=bind(db,t,run);assert bind(db,t,run)==req
    assert db.execute('SELECT allocated_quantity,allowance_quantity,issued_quantity,returned_quantity FROM photo.billing_admission_leases WHERE tenant_id=%s AND resource_code=%s',(t,res)).fetchall()==[(9,0,9,0)]
    assert db.execute('SELECT quantity FROM photo.billing_funding_claims WHERE tenant_id=%s AND resource_code=%s',(t,res)).fetchall()==[(9,)]
    finish(db,case,run,tasks,outcome);assert settle(db);assert not settle(db)
    assert projection(db,t,res)==(9 if outcome=='success' else 0,0);check_conservation(db,t)
    assert db.execute('SELECT coalesce(sum(outstanding_quantity+settled_quantity),0) FROM photo.billing_allowance_budgets WHERE tenant_id=%s AND resource_code=%s',(t,res)).fetchone()==(0,)

def test_paid_operation_above_chunk_cannot_borrow_allowance_for_known_deficit(db):
    case,run,tasks,res=inventory(db,quantity=8);t=case['Tenant']
    db.execute('UPDATE photo.billing_operation_policies SET units_per_success=9 WHERE resource_code=%s',(res,));db.execute('UPDATE photo.billing_admission_policies SET optimistic_allowance=2 WHERE resource_code=%s',(res,))
    rejects(db,'SELECT photo.billing_bind_operations(%s,%s)',(t,run),'BILLING_QUOTA_EXCEEDED')
    assert db.execute('SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=%s',(t,)).fetchone()==(0,)
    assert db.execute('SELECT count(*) FROM photo.billing_funding_claims WHERE tenant_id=%s AND resource_code=%s',(t,res)).fetchone()==(0,)
    check_conservation(db,t)

def test_paid_operation_above_chunk_concurrent_demands_cannot_double_spend(database):
    with psycopg.connect(**database)as c:
        case,run,tasks,res=inventory(c,quantity=9);t=case['Tenant'];dbargs=(t,)
        c.execute('UPDATE photo.billing_operation_policies SET units_per_success=9 WHERE resource_code=%s',(res,));run2,tasks2=next_run(c,case)
    results=race(database,[lambda c,r=r:bind(c,t,r)for r in [run,run2]])
    assert sum(r[0]=='ok'for r in results)==1,results
    assert sum(r[0]=='error'and 'BILLING_QUOTA_EXCEEDED'in r[2]for r in results)==1,results
    with psycopg.connect(**database)as c:
        assert c.execute('SELECT count(*),sum(quantity) FROM photo.billing_funding_claims WHERE tenant_id=%s AND resource_code=%s',(t,res)).fetchone()==(1,9)
        check_conservation(c,t)

def test_paid_operation_allocator_replay_is_item_pinned_and_not_current_price(db):
    case,run,tasks,res=inventory(db,quantity=100);t=case['Tenant'];db.execute('UPDATE photo.billing_operation_policies SET units_per_success=9 WHERE resource_code=%s',(res,));bind(db,t,run)
    item,period,lid,generation=db.execute('SELECT i.id,r.period_id,l.id,l.generation FROM photo.billing_operation_items i JOIN photo.billing_operation_requests r ON r.tenant_id=i.tenant_id AND r.id=i.request_id JOIN photo.billing_funding_claims f ON f.id=i.claim_id JOIN photo.billing_admission_leases l ON l.id=f.lease_id WHERE i.tenant_id=%s',(t,)).fetchone()
    args=(t,res,period,'operation/'+str(run),generation,1,'op/'+str(item),9)
    events=db.execute('SELECT count(*) FROM photo.billing_resource_ledger WHERE tenant_id=%s',(t,)).fetchone()
    db.execute('UPDATE photo.billing_operation_policies SET units_per_success=12 WHERE resource_code=%s',(res,))
    assert db.execute('SELECT photo.billing_allocate_paid_operation_lease('+','.join(['%s']*8)+')',args).fetchone()==(lid,)
    assert db.execute('SELECT count(*) FROM photo.billing_resource_ledger WHERE tenant_id=%s',(t,)).fetchone()==events
    rejects(db,'SELECT photo.billing_allocate_paid_operation_lease('+','.join(['%s']*8)+')',args[:-1]+(10,),'BILLING_PAID_OPERATION_SCOPE_INVALID')
    wrong=list(args);wrong[3]='operation/'+str(uuid4());rejects(db,'SELECT photo.billing_allocate_paid_operation_lease('+','.join(['%s']*8)+')',tuple(wrong),'BILLING_PAID_OPERATION_SCOPE_INVALID')
    check_conservation(db,t)

def test_paid_operation_allocator_is_internal_only(db):
    signature='photo.billing_allocate_paid_operation_lease(uuid,text,uuid,text,bigint,integer,text,bigint)'
    for role in ['photo_api','photo_scheduler','photo_worker','photo_auth','photo_portal','photo_maintenance']:
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(False,)
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api');db.execute('SELECT photo.billing_allocate_paid_operation_lease(NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)')
