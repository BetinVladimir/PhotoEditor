"""Editor funding against real origin/seal SQL authority; controlled canonical output metadata, no pixels."""
from uuid import uuid4
from hashlib import sha256
import json,pytest,psycopg
from psycopg.types.json import Jsonb
from billing_editor_fixture import editor_inventory
from test_billing_apply import enroll,consumed
from test_billing_quota import check_conservation,rejects,race
pytestmark=pytest.mark.migration_head(2607)

def bind(c,t,root,actor,session,project,quantity=20,key=None):
    if not c.execute('SELECT 1 FROM photo.billing_editor_controls WHERE tenant_id=%s',(t,)).fetchone():
        enroll(c,t,quantity)
        c.execute('INSERT INTO photo.billing_editor_controls(tenant_id,enabled) VALUES(%s,true)',(t,))
    key=key or str(root)
    with c.transaction():
        c.execute('SET LOCAL ROLE photo_api')
        try:return c.execute('SELECT photo.billing_bind_editor(%s,%s,%s,%s,%s,%s,%s,%s)',(t,root,actor,session,key,'Editor.Effects.'+str(project),sha256(key.encode()).digest(),'editor-api')).fetchone()[0]
        finally:
            # Error rollback must preserve the original error, not mask it with RESET ROLE.
            pass

def admit(c,*args,**kw):
    try:return bind(c,*args,**kw)
    finally:
        if c.info.transaction_status!=psycopg.pq.TransactionStatus.INERROR:c.execute('RESET ROLE')

def settle(c):
    with c.transaction():
        c.execute('SET LOCAL ROLE photo_scheduler')
        result=c.execute('SELECT photo.billing_settle_next_editor()').fetchone()[0]
        c.execute('RESET ROLE');return result

def inventory(c,quantity=20):return editor_inventory(c,admit=lambda *args:admit(*args,quantity=quantity))

def complete_final(c,t,root,final,version):
    # Final fixture task is render.compose with deterministic Editor child identity.
    old=c.execute('SELECT id,params_artifact_id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',(t,final)).fetchone()
    c.execute('DELETE FROM photo.tasks WHERE id=%s',(old[0],))
    task=c.execute('SELECT photo.editor_effect_final_task_id(%s)',(root,)).fetchone()[0]
    workflow=uuid4()
    c.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) SELECT %s,tenant_id,(SELECT project_id FROM photo.editor_effect_builds WHERE run_id=%s),id,input_snapshot_id,'preview','v2.2',%s FROM photo.runs WHERE id=%s",(workflow,root,'a'*64,final))
    c.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'render.compose:0','render.compose','1','gpu','blocked','[]',%s,%s,'{\"renderer_version\":\"v2.2\"}',1,1)",(task,t,final,old[1],'a'*64))
    c.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cuda','{}')",(t,task,uuid4()))
    outputs=[]
    project=c.execute('SELECT project_id FROM photo.editor_effect_builds WHERE run_id=%s',(root,)).fetchone()[0]
    for suffix in ['','/working_image']:
        artifact=c.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'fixture',%s,'v1',%s,64,'application/json','document',now(),%s,1,1) RETURNING id",(t,str(uuid4()),'a'*64,task)).fetchone()[0]
        outputs.append(artifact)
        c.execute('INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,%s,0)',(t,project,workflow,artifact,'a'*64,sha256(('tasks/'+str(task)+'/$'+suffix).encode()).hexdigest()))
    c.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,'document',bucket,object_key,sha256,bytes,media_type,'ready',object_version,now() FROM photo.artifacts WHERE id=%s",(task,uuid4(),'a'*64,uuid4(),outputs[0]))
    result=c.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(t,task,Jsonb([dict(artifact_id=str(outputs[0]),sha256='a'*64)]),'c'*64)).fetchone()[0]
    c.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    c.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),now())",(t,task,result))
    c.execute("UPDATE photo.runs SET status='completed',graph_sealed=true WHERE id=%s",(final,))
    c.execute('SELECT photo.billing_link_editor_children(%s,%s)',(t,root))
    c.execute('SET LOCAL ROLE photo_scheduler')
    assert c.execute('SELECT photo.settle_editor_effect_final_render()').fetchone()==(True,)
    c.execute('RESET ROLE')
    return task

def test_editor_intermediate_apply_neither_charges_nor_refunds(db):
    t,root,final,pending,_,version,*_=inventory(db)
    assert consumed(db,t)==(0,0)
    db.execute('SELECT photo.billing_settle_apply_run(%s,%s)',(t,root))
    assert db.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==('accepted',)
    assert not settle(db)
    complete_final(db,t,root,final,version)
    assert settle(db);assert not settle(db)
    assert consumed(db,t)==(1,0)
    assert db.execute('SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=%s',(t,)).fetchone()==(1,)
    db.execute('INSERT INTO photo.billing_render_controls(tenant_id,enabled) VALUES(%s,true)',(t,))
    rejects(db,'SELECT photo.billing_bind_render(%s,%s,%s,%s,%s)',(t,final,*db.execute('SELECT actor_id,session_id FROM photo.editor_effect_builds WHERE run_id=%s',(root,)).fetchone(),'child-api'),'BILLING_CHILD_NOT_ROOT')
    check_conservation(db,t)

def test_editor_failure_drains_before_refund(db):
    t,root,final,pending,_,version,*_=inventory(db)
    db.execute("UPDATE photo.runs SET status='failed' WHERE id=%s",(root,))
    db.execute('SELECT photo.withdraw_editor_effect_build()')
    assert not settle(db)
    db.execute("UPDATE photo.tasks SET state='cancelled',terminal_reason='fixture_cancel' WHERE id=%s",(pending,))
    for _ in range(4):db.execute('SELECT photo.finalize_run_cancellation()')
    db.execute('SELECT photo.withdraw_editor_effect_build()')
    assert settle(db);assert not settle(db)
    assert consumed(db,t)==(0,0)
    assert db.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==('released',)
    for r in db.execute('SELECT run_id FROM photo.billing_usage_run_links WHERE tenant_id=%s',(t,)).fetchall():
        rejects(db,"UPDATE photo.runs SET status='running' WHERE id=%s",r,'BILLING_RUN_TERMINAL',psycopg.errors.CheckViolation)
    check_conservation(db,t)

def test_editor_final_missing_proof_holds_funding(db):
    t,root,final,pending,_,version,*_=inventory(db);complete_final(db,t,root,final,version)
    db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",(t,))
    assert not settle(db)
    assert consumed(db,t)==(0,0)
    assert db.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==('accepted',)

def test_editor_binding_replay_current_authority_and_hash_conflict(db):
    t,root,final,pending,_,version,*_=inventory(db)
    actor,session,project=db.execute('SELECT actor_id,session_id,project_id FROM photo.editor_effect_builds WHERE run_id=%s',(root,)).fetchone()
    req=admit(db,t,root,actor,session,project)
    assert admit(db,t,root,actor,session,project)==req
    rejects(db,'SELECT photo.billing_bind_editor(%s,%s,%s,%s,%s,%s,%s,%s)',(t,root,actor,session,str(root),'Editor.Effects.'+str(project),sha256(b'changed').digest(),'api'),'BILLING_IDEMPOTENCY_CONFLICT')
    db.execute('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',(session,))
    rejects(db,'SELECT photo.billing_bind_editor(%s,%s,%s,%s,%s,%s,%s,%s)',(t,root,actor,session,str(root),'Editor.Effects.'+str(project),sha256(str(root).encode()).digest(),'api'),'BILLING_AUTHORITY_INVALID')

@pytest.mark.parametrize('action',['bind','settle','refund'])
def test_editor_concurrent_replays(database,action):
    with psycopg.connect(**database,autocommit=True) as c:
        with c.transaction():
            t,root,final,pending,_,version,*_=inventory(c)
            if action=='settle':complete_final(c,t,root,final,version)
            if action=='refund':
                c.execute("UPDATE photo.runs SET status='failed' WHERE id=%s",(root,))
                c.execute('SELECT photo.withdraw_editor_effect_build()')
                c.execute("UPDATE photo.tasks SET state='cancelled',terminal_reason='fixture_cancel' WHERE id=%s",(pending,))
                for _ in range(4):c.execute('SELECT photo.finalize_run_cancellation()')
                c.execute('SELECT photo.withdraw_editor_effect_build()')
            actor,session,project=c.execute('SELECT actor_id,session_id,project_id FROM photo.editor_effect_builds WHERE run_id=%s',(root,)).fetchone()
        results=race(database,[lambda cx:settle(cx) if action!='bind' else admit(cx,t,root,actor,session,project) for _ in range(6)])
        assert all(x[0]=='ok' for x in results),results
        if action!='bind':assert sum(x[1] for x in results)==1
        else:assert len({x[1] for x in results})==1
        assert consumed(c,t)==((1,0) if action=='settle' else (0,0))
        check_conservation(c,t)

@pytest.mark.parametrize('damage',['adoption','root_expired','image_unverified','source_withdrawn'])
def test_editor_uncertain_final_never_refunds_or_consumes(db,damage):
    t,root,final,_,_,version,*_=inventory(db);task=complete_final(db,t,root,final,version)
    if damage=='adoption':db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",(t,))
    elif damage=='root_expired':db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE created_by_task_id=%s",(task,))
    elif damage=='image_unverified':db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE created_by_task_id=%s',(task,))
    else:db.execute('UPDATE photo.assets SET deleted_at=now() WHERE id=(SELECT asset_id FROM photo.asset_versions WHERE id=%s)',(version,))
    assert not settle(db)
    assert consumed(db,t)==(0,0)
    assert db.execute('SELECT state FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==('accepted',)
    check_conservation(db,t)

def test_editor_no_paid_quota_uses_only_finite_allowance(db):
    t,root,final,_,_,version,*_=inventory(db,quantity=0)
    complete_final(db,t,root,final,version);assert settle(db)
    assert consumed(db,t)==(1,1)
    check_conservation(db,t)

def test_editor_control_defaults_off_and_internal_functions_private(db):
    t,root,*_=editor_inventory(db)
    actor,session,project=db.execute('SELECT actor_id,session_id,project_id FROM photo.editor_effect_builds WHERE run_id=%s',(root,)).fetchone()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT photo.billing_prepare_editor(%s,%s,%s,%s)',(t,actor,session,project)).fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=%s',(t,)).fetchone()==(0,)
    for signature in ['billing_link_editor_children(uuid,uuid)','billing_settle_render(uuid,uuid)','begin_editor_final_before_billing(uuid,uuid,uuid,uuid,text,text,bigint,jsonb)','seal_v2_graph_before_editor_billing(uuid,uuid)']:
        for role in ['photo_api','photo_worker','photo_scheduler']:
            assert not db.execute("SELECT has_function_privilege(%s,'photo.'||%s,'EXECUTE')",(role,signature)).fetchone()[0]

def test_real_go_editor_admission_replay_and_rollback(database):
    import os,subprocess
    from pathlib import Path
    from psycopg.conninfo import make_conninfo
    with psycopg.connect(**database) as c:
        t,root,*_=editor_inventory(c)
        actor,session,project,epoch=c.execute('SELECT b.actor_id,b.session_id,b.project_id,u.auth_epoch FROM photo.editor_effect_builds b JOIN photo.users u ON u.id=b.actor_id WHERE b.run_id=%s',(root,)).fetchone()
        enroll(c,t)
        c.execute('INSERT INTO photo.billing_editor_controls(tenant_id,enabled) VALUES(%s,true)',(t,))
    fixture=dict(Tenant=str(t),Root=str(root),Actor=str(actor),Session=str(session),Project=str(project),Epoch=epoch)
    env=dict(os.environ,PHOTO_BILLING_EDITOR_FIXTURE=json.dumps(fixture),PHOTO_TEST_DATABASE_URL=make_conninfo(**database))
    result=subprocess.run(['/usr/local/go/bin/go','test','./internal/run','-run','^TestIntegrationBillingEditorAdmission$','-count=1','-v'],cwd=Path(__file__).resolve().parents[2]/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr

def next_editor(c,t,origin):
    root=c.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) SELECT tenant_id,'apply','queued',input_snapshot_id,'{}',config_hash,created_by FROM photo.runs WHERE id=%s RETURNING id",(origin,)).fetchone()[0]
    c.execute("INSERT INTO photo.apply_builds(tenant_id,run_id,source_collection_id,item_count,phase,graph_version) VALUES(%s,%s,NULL,1,'resolve',4)",(t,root))
    c.execute("INSERT INTO photo.editor_effect_builds(tenant_id,run_id,project_id,actor_id,session_id,asset_version_id,source_artifact_id,source_sha,source_canvas_hash,frozen_recipe_artifact_id,frozen_recipe_sha,frozen_document_id,draft_document_id,draft_revision,compilation_sha,executed_style_artifact_id,executed_style_sha) SELECT tenant_id,%s,project_id,actor_id,session_id,asset_version_id,source_artifact_id,source_sha,source_canvas_hash,frozen_recipe_artifact_id,frozen_recipe_sha,frozen_document_id,draft_document_id,draft_revision,compilation_sha,executed_style_artifact_id,executed_style_sha FROM photo.editor_effect_builds WHERE run_id=%s",(root,origin))
    return root

def test_editor_rerun_slots_and_exhausted_optimistic_budget(db):
    t,root,final,_,_,version,*_=inventory(db,quantity=0)
    complete_final(db,t,root,final,version);assert settle(db)
    actor,session,project=db.execute('SELECT actor_id,session_id,project_id FROM photo.editor_effect_builds WHERE run_id=%s',(root,)).fetchone()
    for _ in range(2):
        r=next_editor(db,t,root);admit(db,t,r,actor,session,project,quantity=0)
    r=next_editor(db,t,root)
    with pytest.raises(psycopg.Error,match='BILLING_OVERRUN_BLOCKED'):
        with db.transaction():admit(db,t,r,actor,session,project,quantity=0)
    assert consumed(db,t)==(1,1)
    assert db.execute('SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=%s',(t,)).fetchone()==(3,)
    check_conservation(db,t)

def test_editor_independent_initial_requests_serialize(database):
    with psycopg.connect(**database) as c:
        t,origin,*_=editor_inventory(c);enroll(c,t)
        c.execute('INSERT INTO photo.billing_editor_controls(tenant_id,enabled) VALUES(%s,true)',(t,))
        actor,session,project=c.execute('SELECT actor_id,session_id,project_id FROM photo.editor_effect_builds WHERE run_id=%s',(origin,)).fetchone()
        roots=[next_editor(c,t,origin) for _ in range(6)]
    results=race(database,[lambda cx,r=r: (cx.execute("SELECT set_config('app.tenant_id',%s,true)",(str(t),)),admit(cx,t,r,actor,session,project))[1] for r in roots])
    assert sum(x[0]=='ok' for x in results)==1,results
    assert all(x[0]=='ok' or 'PHOTO_INITIAL_IN_PROGRESS' in x[2] for x in results),results
    with psycopg.connect(**database) as c:check_conservation(c,t)


def test_editor_final_seal_requires_accepted_root_ticket(db):
    t,root,final,*_=editor_inventory(db)
    db.execute('INSERT INTO photo.billing_editor_controls(tenant_id,enabled) VALUES(%s,true)',(t,))
    assert db.execute('SELECT photo.seal_v2_graph(%s,%s)',(t,final)).fetchone()==(False,)
    assert db.execute('SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=%s',(t,)).fetchone()==(0,)
