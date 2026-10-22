"""Authored residency publication boundary, NOT RUN."""
from uuid import uuid4
import pytest
import psycopg
from psycopg.types.json import Jsonb

@pytest.mark.parametrize('role,allowed',[('photo_worker',True),('photo_scheduler',False),('photo_api',False)])
def test_residency_report_worker_only(db,role,allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.report_worker_model_residency(uuid,text,jsonb)','EXECUTE')",(role,)).fetchone()==(allowed,)

@pytest.mark.parametrize('value',[None,{},[{}],[{'model_lock_sha':'bad'}]])
def test_residency_report_invalid_shape(db,value):
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():
            db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(uuid4(),'a'*64,Jsonb(value)))

def test_residency_report_missing_registration_refuses(db):
    assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(uuid4(),'a'*64,Jsonb([]))).fetchone()==(False,)


@pytest.mark.parametrize('role,allowed',[('photo_scheduler',True),('photo_worker',False),('photo_api',False)])
def test_affinity_nodes_scheduler_only(db,role,allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.capacity_affinity_dispatch_nodes(uuid)','EXECUTE')",(role,)).fetchone()==(allowed,)


def test_affinity_nodes_missing_task_is_empty(db):
    assert db.execute('SELECT * FROM photo.capacity_affinity_dispatch_nodes(%s)',(uuid4(),)).fetchall()==[]


@pytest.mark.parametrize('change',["enabled=false", "lock_id='cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'", "backends='[\"mps\"]'::jsonb", "qualified_operations='{}'::jsonb", "capacity_policy_sha=NULL"])
def test_residency_authority_change_withdraws_cache_hint(db,change):
    # Trigger scope only: operator-seeded hint is not a qualified report proof.
    from test_v32_capacity_heavy_inflight import _scope
    from test_v32_capacity_rotation_concurrency import _install
    tenant,_,_,_=_scope(db)
    pin=_install(db,60)
    worker=uuid4();node='residency-'+uuid4().hex
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,1000,0,1000)',(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,enabled,qualified_operations,capacity_policy_sha,model_residency,model_residency_reported_at) VALUES(%s,%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',session_user,true,'{\"asset.verify\":[\"cpu\"]}',%s,%s,clock_timestamp())",(tenant,worker,node,'b'*64,pin,Jsonb([{'model_lock_sha':'a'*64}])) )
    db.execute('UPDATE photo.worker_registrations SET last_seen=clock_timestamp(),capacity_policy_sha=capacity_policy_sha WHERE worker_id=%s',(worker,))
    assert db.execute('SELECT jsonb_array_length(model_residency),model_residency_reported_at IS NOT NULL FROM photo.worker_registrations WHERE worker_id=%s',(worker,)).fetchone()==(1,True)
    # change is from the fixed test list above; no external SQL input.
    db.execute('UPDATE photo.worker_registrations SET '+change+' WHERE worker_id=%s',(worker,))
    assert db.execute('SELECT model_residency,model_residency_reported_at FROM photo.worker_registrations WHERE worker_id=%s',(worker,)).fetchone()==([],None)


@pytest.mark.parametrize('mode',['valid','stale','policy','disabled','principal'])
def test_model_residency_report_registered_worker_lifecycle(db,mode):
    from test_v32_capacity_heavy_inflight import _scope
    from test_v32_capacity_rotation_concurrency import _install
    tenant,_,_,_=_scope(db)
    pin=_install(db,60)
    worker=uuid4();node='publish-'+uuid4().hex
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,1000,0,1000)',(node,))
    principal=db.execute('SELECT session_user').fetchone()[0]
    registration=db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,enabled,qualified_operations,capacity_policy_sha,reported_capacity_policy_sha,capacity_policy_reported_at) VALUES(%s,%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',%s,true,'{\"asset.verify\":[\"cpu\"]}',%s,%s,clock_timestamp()) RETURNING id",(tenant,worker,node,'a'*64,principal,pin,pin)).fetchone()[0]
    if mode=='stale':
        db.execute("UPDATE photo.worker_registrations SET last_seen=clock_timestamp()-interval '1 hour' WHERE id=%s",(registration,))
    elif mode=='disabled':
        db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE id=%s',(registration,))
    elif mode=='principal':
        db.execute('UPDATE photo.worker_registrations SET db_principal=%s WHERE id=%s',('foreign_'+uuid4().hex,registration))
    report=[{'model_lock_sha':'a'*64,'model_id':'u2net','backend':'cpu','qualification_report_sha':'b'*64,'generation':1}]
    reported_pin='c'*64 if mode=='policy' else pin
    db.execute('SET LOCAL ROLE photo_worker')
    try:
        assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(worker,reported_pin,Jsonb(report))).fetchone()==(mode=='valid',)
    finally:
        db.execute('RESET ROLE')
    row=db.execute('SELECT model_residency,model_residency_reported_at FROM photo.worker_registrations WHERE id=%s',(registration,)).fetchone()
    if mode!='valid':
        assert row==([],None)
    else:
        assert row[0]==report and row[1] is not None
        db.execute('SET LOCAL ROLE photo_worker')
        try:
            assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(worker,pin,Jsonb([]))).fetchone()==(True,)
        finally:
            db.execute('RESET ROLE')
        row=db.execute('SELECT model_residency,model_residency_reported_at FROM photo.worker_registrations WHERE id=%s',(registration,)).fetchone()
        assert row[0]==[] and row[1] is not None


@pytest.mark.parametrize('generation',[-1,0.5,9007199254740992,None,True])
def test_residency_invalid_generation_refuses_before_registration(db,generation):
    report={'model_lock_sha':'a'*64,'model_id':'u2net','backend':'cpu','qualification_report_sha':'b'*64,'generation':generation}
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():
            db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(uuid4(),'c'*64,Jsonb([report])))


def test_residency_duplicate_models_refuse(db):
    report={'model_lock_sha':'a'*64,'model_id':'u2net','backend':'cpu','qualification_report_sha':'b'*64,'generation':1}
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():
            db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(uuid4(),'c'*64,Jsonb([report,dict(report,generation=2)])))


@pytest.mark.parametrize('role,allowed',[('photo_scheduler',True),('photo_worker',False),('photo_api',False)])
def test_task_affinity_scheduler_only(db,role,allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.capacity_task_affinity(uuid[])','EXECUTE')",(role,)).fetchone()==(allowed,)


def test_task_affinity_missing_or_empty_scope_has_no_hint(db):
    assert db.execute('SELECT * FROM photo.capacity_task_affinity(%s::uuid[])',([],)).fetchall()==[]
    assert db.execute('SELECT * FROM photo.capacity_task_affinity(%s::uuid[])',([uuid4()],)).fetchall()==[]


@pytest.mark.parametrize('mode',['fresh','stale','future','wrong_lock','wrong_model','withdrawn','disabled'])
def test_affinity_real_two_node_residency_scope(db,mode):
    from test_v32_capacity_heavy_inflight import _scope
    from test_v32_capacity_rotation_concurrency import _install
    tenant,user,artifact,snapshot=_scope(db)
    pin=_install(db,60)
    prefix='affinity-'+uuid4().hex
    nodes=[prefix+'-a',prefix+'-b']
    workers=[uuid4(),uuid4()]
    principal=db.execute('SELECT session_user').fetchone()[0]
    for node,worker in zip(nodes,workers):
        db.execute('INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES(%s,true,10000000,0,10000000,10000000,10000000,clock_timestamp(),%s)',(node,'b'*64))
        db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'cpu1','cpu','{}')",(node,))
        db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,enabled,qualified_operations,capacity_policy_sha,reported_capacity_policy_sha,capacity_policy_reported_at) VALUES(%s,%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',%s,true,'{\"face.detect\":[\"cpu\"]}',%s,%s,clock_timestamp())",(tenant,worker,node,'a'*64,principal,pin,pin))
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,failure_policy) VALUES(%s,'v2','running',%s,%s,%s,%s,true,'fail_fast') RETURNING id",(tenant,snapshot,Jsonb({'queue_class':'batch'}),'b'*64,user)).fetchone()[0]
    task=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,'affinity:0','face.detect','1','cpu','ready','[]',%s,%s,%s) RETURNING id",(tenant,run,artifact,'b'*64,Jsonb({'backend':'cpu','model_lock_sha':'a'*64,'resource_estimate':{'ram_bytes':2048,'vram_bytes':0,'scratch_bytes':1024,'output_bytes':0,'pixel_count':0}}))).fetchone()[0]
    baseline=db.execute('SELECT node_id FROM photo.eligible_dispatch_nodes(%s)',(task,)).fetchall()
    assert baseline==[(nodes[0],),(nodes[1],)]
    report=[{'model_lock_sha':'c'*64 if mode=='wrong_lock' else 'a'*64,'model_id':'fan2d' if mode=='wrong_model' else 'sfd','backend':'cpu','qualification_report_sha':'d'*64,'generation':1}]
    db.execute('SET LOCAL ROLE photo_worker')
    assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(workers[1],pin,Jsonb(report))).fetchone()==(mode!='wrong_lock',)
    if mode=='withdrawn':
        assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(workers[1],pin,Jsonb([]))).fetchone()==(True,)
    db.execute('RESET ROLE')
    if mode=='stale':
        db.execute("UPDATE photo.worker_registrations SET model_residency_reported_at=clock_timestamp()-interval '1 hour' WHERE worker_id=%s",(workers[1],))
    if mode=='future':
        db.execute("UPDATE photo.worker_registrations SET model_residency_reported_at=clock_timestamp()+interval '1 hour' WHERE worker_id=%s",(workers[1],))
    if mode=='disabled':
        db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(workers[1],))
    db.execute('SET LOCAL ROLE photo_scheduler')
    ordered=db.execute('SELECT node_id FROM photo.capacity_affinity_dispatch_nodes(%s)',(task,)).fetchall()
    hints=db.execute('SELECT * FROM photo.capacity_task_affinity(%s::uuid[])',([task,uuid4()],)).fetchall()
    if mode=='fresh':
        assert ordered==[(nodes[1],),(nodes[0],)]
        assert hints==[(task,'a'*64,True)]
    else:
        expected=[(nodes[0],)] if mode=='disabled' else baseline
        assert ordered==expected
        assert hints==[]
    assert set(ordered)==set(db.execute('SELECT node_id FROM photo.eligible_dispatch_nodes(%s)',(task,)).fetchall())



@pytest.mark.parametrize('change,accepted',[({},True),({'model_lock_sha':'c'*64},False),({'backend':'cuda'},False)])
def test_residency_report_binds_current_registration_lock_and_backend(db,change,accepted):
    from test_task_leases import queued_task
    from test_v32_capacity_rotation_concurrency import _install
    item=queued_task(db)
    pin=_install(db,60)
    db.execute('UPDATE photo.worker_registrations SET lock_id=%s,capacity_policy_sha=%s,'
        'reported_capacity_policy_sha=%s,capacity_policy_reported_at=clock_timestamp(),last_seen=clock_timestamp() '
        'WHERE worker_id=%s',('a'*64,pin,pin,item['worker']))
    model={**dict(model_lock_sha='a'*64,model_id='sfd',backend='cpu',qualification_report_sha='b'*64,generation=1),**change}
    assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',
        (item['worker'],pin,Jsonb([model]))).fetchone()==(accepted,)
    row=db.execute('SELECT model_residency,model_residency_reported_at FROM photo.worker_registrations '
        'WHERE worker_id=%s',(item['worker'],)).fetchone()
    if accepted:
        assert row[0]==[model] and row[1] is not None
    else:
        assert row==([],None)


def test_rotated_model_lock_cannot_restore_old_residency_hint(db):
    from test_task_leases import queued_task
    from test_v32_capacity_rotation_concurrency import _install
    item=queued_task(db);pin=_install(db,60)
    db.execute('UPDATE photo.worker_registrations SET lock_id=%s,capacity_policy_sha=%s,'
        'reported_capacity_policy_sha=%s,capacity_policy_reported_at=clock_timestamp(),last_seen=clock_timestamp() '
        'WHERE worker_id=%s',('a'*64,pin,pin,item['worker']))
    model=dict(model_lock_sha='a'*64,model_id='sfd',backend='cpu',qualification_report_sha='b'*64,generation=1)
    assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(item['worker'],pin,Jsonb([model]))).fetchone()==(True,)
    db.execute('UPDATE photo.worker_registrations SET lock_id=%s WHERE worker_id=%s',('c'*64,item['worker']))
    assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(item['worker'],pin,Jsonb([model]))).fetchone()==(False,)
    assert db.execute('SELECT model_residency,model_residency_reported_at FROM photo.worker_registrations '
        'WHERE worker_id=%s',(item['worker'],)).fetchone()==([],None)
    assert db.execute('SELECT photo.report_worker_model_residency(%s,%s,%s)',(item['worker'],pin,Jsonb([]))).fetchone()==(True,)
