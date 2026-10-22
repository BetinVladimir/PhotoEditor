"""Own removal dispatch/claim metadata fixture. No worker/object-store execution."""
import json
from psycopg.types.json import Jsonb
from removal_apply_documents_fixture import committed_copy_fixture

def dispatched_copy_fixture(db, refusal=None, *, include_documents=False):
    """Run actual scheduler reservation before exercising actual worker claim."""
    from uuid import uuid4
    values, task, _ = committed_copy_fixture(db)
    tenant = values[0][0][0]
    task_id = task['node']['id']
    run, params, sha, fence, profile, inputs = db.execute(
        'SELECT t.run_id,t.params_artifact_id,a.sha256,t.fencing_token,t.execution_profile,t.inputs '
        'FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id '
        'WHERE t.id=%s', (task_id,)).fetchone()
    worker = uuid4()
    node = 'removal-claim-' + str(uuid4())
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,enabled,reported_at,doctor_report_sha256) VALUES(%s,%s,0,%s,%s,0,%s,true,clock_timestamp(),%s)',
        (node,2**40,2**40,2**40,2**40,'a'*64))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) "
        "VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',session_user,%s,true,'{\"semantic.adopt_shown_removal\":[\"cpu\"]}','[\"v2.2\"]')",
        (tenant,worker,profile.get('model_lock_id','a'*64),node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'cpu1','cpu','{}')",(node,))
    estimate = profile['resource_estimate']
    event, correlation = uuid4(), uuid4()
    expected_refusals = dict(stale_capacity='waiting_resource',insufficient_ram='waiting_resource',
        unqualified_worker='waiting_worker',unavailable_input='input_unavailable')
    if refusal=='stale_capacity':
        db.execute("UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '1 hour' WHERE node_id=%s",(node,))
    elif refusal=='insufficient_ram':
        db.execute('UPDATE photo.resource_nodes SET ram_available_bytes=1 WHERE node_id=%s',(node,))
    elif refusal=='unqualified_worker':
        db.execute("UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE tenant_id=%s AND worker_id=%s",(tenant,worker))
    elif refusal=='unavailable_input':
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(inputs[0]['artifact_id'],))
    elif refusal is not None:
        raise AssertionError('Unknown dispatch refusal')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        dispatched = db.execute('SELECT * FROM photo.reserve_dispatch(%s,%s,%s,0,%s,%s,%s,%s)',
            (task_id,node,estimate['ram_bytes'],estimate['scratch_bytes'],event,correlation,'')).fetchone()
        assert dispatched[0]==expected_refusals.get(refusal,'queued')
        db.execute('RESET ROLE')
    if refusal is not None:
        assert dispatched[1] is None
        assert db.execute('SELECT state,attempt,fencing_token,dispatch_command,dispatch_worker_id FROM photo.tasks WHERE id=%s',(task_id,)).fetchone()==('ready',0,fence,None,None)
        assert db.execute('SELECT count(*) FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',(tenant,task_id)).fetchone()==(0,)
        assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task_id,)).fetchone()==(0,)
        assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()==(0,)
        return
    command = dispatched[1]
    assert command['inputs']==inputs
    assert command['params']==dict(artifact_id=str(params),sha256=sha)
    assert command['operation']=='semantic.adopt_shown_removal'
    assert command['model'] is None and command['attempt']==1
    assert db.execute('SELECT dispatch_worker_id,dispatch_command FROM photo.tasks WHERE id=%s',(task_id,)).fetchone()==(worker,command)
    route,payload = db.execute('SELECT routing_key,payload FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()
    assert route=='worker.'+str(worker)+'.cpu.semantic_adopt_shown_removal'
    assert payload==command
    assert db.execute('SELECT ram_bytes,vram_bytes,scratch_bytes,fencing_token FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',
        (tenant,task_id)).fetchone()==(estimate['ram_bytes'],0,estimate['scratch_bytes'],fence)
    result=(tenant,task_id,run,worker,command,fence)
    return (*result,values,task) if include_documents else result

