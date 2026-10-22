"""Real dispatch/claim SQL with synthetic documents; no storage/GPU proof."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_background_cleanup_preview_activation import published_transport_fixture,commit_preview


def dispatched_preview_fixture(db,refusal=None):
    args,run,scope=published_transport_fixture(db)
    task=commit_preview(db,args,run,scope)
    tenant=args[0]
    profile,inputs,params,sha,fence=db.execute(
        'SELECT t.execution_profile,t.inputs,t.params_artifact_id,a.sha256,t.fencing_token '
        'FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id '
        'WHERE t.id=%s',(task,)).fetchone()
    # Explicit test-only deployment activation, rolled back by database fixture.
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='semantic.preview_background_cleanup' AND version='1'")
    worker=uuid4();node='cleanup-preview-'+str(uuid4())
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,enabled,reported_at,doctor_report_sha256) '
        'VALUES(%s,%s,0,%s,%s,0,%s,true,clock_timestamp(),%s)',(node,2**40,2**40,2**40,2**40,'a'*64))
    db.execute('INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) '
        'VALUES(%s,%s,%s,%s,%s,clock_timestamp(),%s,session_user,%s,true,%s,%s)',
        (tenant,worker,Jsonb(['gpu']),Jsonb(['cpu']),profile['model_lock_id'],Jsonb({}),node,
         Jsonb({'semantic.preview_background_cleanup':['cpu']}),Jsonb(['v2.2'])))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'gpu1','gpu','{}')",(node,))
    if refusal=='stale_capacity':
        db.execute("UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '1 hour' WHERE node_id=%s",(node,))
    elif refusal=='unqualified_worker':
        db.execute("UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE tenant_id=%s AND worker_id=%s",(tenant,worker))
    elif refusal=='unavailable_input':
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(inputs[0]['artifact_id'],))
    elif refusal is not None:raise AssertionError('unknown refusal')
    estimate=profile['resource_estimate'];event,correlation=uuid4(),uuid4()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        dispatched=db.execute('SELECT * FROM photo.reserve_dispatch(%s,%s,%s,0,%s,%s,%s,%s)',
            (task,node,estimate['ram_bytes'],estimate['scratch_bytes'],event,correlation,'')).fetchone()
        db.execute('RESET ROLE')
    if refusal:
        assert dispatched[0]=={'stale_capacity':'waiting_resource','unqualified_worker':'waiting_worker','unavailable_input':'input_unavailable'}[refusal]
        assert dispatched[1] is None
        assert db.execute('SELECT state,attempt,fencing_token,dispatch_command FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('ready',0,fence,None)
        assert db.execute('SELECT count(*) FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()==(0,)
        assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()==(0,)
        return
    assert dispatched[0]=='queued'
    command=dispatched[1]
    assert command['operation']=='semantic.preview_background_cleanup' and command['attempt']==1
    assert command['params']=={'artifact_id':str(params),'sha256':sha}
    assert command['inputs']==inputs
    route,payload=db.execute('SELECT routing_key,payload FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()
    assert route=='worker.'+str(worker)+'.gpu.semantic_preview_remove_object'
    assert payload==command
    assert db.execute('SELECT ram_bytes,vram_bytes,scratch_bytes,fencing_token FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',
        (tenant,task)).fetchone()==(estimate['ram_bytes'],0,estimate['scratch_bytes'],fence)
    return tenant,task,run,worker,command,fence,profile


def test_cleanup_preview_dispatch_claim_starts_run_and_rejects_duplicate(database):
    tenant,task,run,worker,command,fence,profile=dispatched_preview_fixture(database)
    before=database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        claimed=database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()
        assert claimed[0]=='claimed' and claimed[1]==fence+1
        assert claimed[4]['command']==command
        assert database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()[0]=='busy'
        database.execute('RESET ROLE')
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()==('running',before[1]+(before[0]=='queued'))
    assert database.execute('SELECT state,attempt,fencing_token,worker_id,execution_profile FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('running',1,fence+1,worker,profile)
    assert database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(task,)).fetchone()==(1,)


@pytest.mark.parametrize('refusal',['stale_capacity','unqualified_worker','unavailable_input'])
def test_cleanup_preview_dispatch_refusal_has_no_partial_reservation(database,refusal):
    dispatched_preview_fixture(database,refusal)
