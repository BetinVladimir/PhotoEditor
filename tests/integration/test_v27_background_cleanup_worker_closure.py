"""Persisted closure and real claim/start SQL; no physical-object or GPU proof."""
import json

import pytest

from psycopg.types.json import Jsonb
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from background_cleanup_apply_documents_fixture import committed_copy_fixture


def test_background_cleanup_worker_closure_requires_exact_persisted_inputs(database):
    values,task,response = committed_copy_fixture(database)
    args = values[0][0]
    def ready():
        return database.execute('SELECT photo.semantic_shown_background_cleanup_closure_ready(%s,%s)',
            (task['node']['id'],Jsonb(json.loads(task['params_body'])))).fetchone()[0]
    assert ready() is True
    with database.transaction(force_rollback=True):
        database.execute('DELETE FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s AND contract=%s',
            (args[0],task['node']['id'],'InputClosureChunk'))
        assert ready() is False
    with database.transaction(force_rollback=True):
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(task['inventory']['entries'][0]['ref']['artifact_id'],))
        assert ready() is False
    with database.transaction(force_rollback=True):
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task['node']['id'],))
        assert ready() is False
    assert ready() is True
    assert database.execute('SELECT state,attempt FROM photo.tasks WHERE id=%s',(task['node']['id'],)).fetchone()==('ready',0)


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
    node = 'cleanup-claim-' + str(uuid4())
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,enabled,reported_at,doctor_report_sha256) VALUES(%s,%s,0,%s,%s,0,%s,true,clock_timestamp(),%s)',
        (node,2**40,2**40,2**40,2**40,'a'*64))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) "
        "VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',session_user,%s,true,'{\"semantic.adopt_shown_background_cleanup\":[\"cpu\"]}','[\"v2.2\"]')",
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
    assert command['operation']=='semantic.adopt_shown_background_cleanup'
    assert command['model'] is None and command['attempt']==1
    assert db.execute('SELECT dispatch_worker_id,dispatch_command FROM photo.tasks WHERE id=%s',(task_id,)).fetchone()==(worker,command)
    route,payload = db.execute('SELECT routing_key,payload FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()
    assert route=='worker.'+str(worker)+'.cpu.semantic_adopt_shown_background_cleanup'
    assert payload==command
    assert db.execute('SELECT ram_bytes,vram_bytes,scratch_bytes,fencing_token FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',
        (tenant,task_id)).fetchone()==(estimate['ram_bytes'],0,estimate['scratch_bytes'],fence)
    result=(tenant,task_id,run,worker,command,fence)
    return (*result,values,task) if include_documents else result


def test_background_cleanup_claim_starts_run_and_fences_duplicate(database):
    tenant,task,run,worker,command,old_fence = dispatched_copy_fixture(database)
    before = database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        claimed = database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()
        assert claimed[0]=='claimed' and claimed[1]==old_fence+1
        assert claimed[4]['command']==command
        assert database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()[0]=='busy'
        database.execute('RESET ROLE')
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()==(
        'running',before[1]+(1 if before[0]=='queued' else 0))
    assert database.execute('SELECT state,attempt,fencing_token,worker_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('running',1,old_fence+1,worker)
    assert database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(task,)).fetchone()==(1,)


@pytest.mark.parametrize('mode,code',[('quota','QUOTA'),('inputs','INPUTS'),('empty_input_version','INPUTS'),('null_input_version','INPUTS'),('profile','')])
def test_background_cleanup_claim_failure_rolls_back_base_claim(database,mode,code):
    import pytest
    from psycopg import errors
    tenant,task,run,worker,command,old_fence = dispatched_copy_fixture(database)
    before = database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    if mode=='quota':
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run))
    elif mode in ('empty_input_version','null_input_version'):
        from removal_transaction_state import tenant_transaction_state
        snapshot=tenant_transaction_state(database,tenant)
        with pytest.raises(errors.CheckViolation) as exact_failure:
            with database.transaction():
                database.execute('UPDATE photo.artifacts SET object_version=%s WHERE id=(SELECT artifact_id FROM photo.v2_task_inputs WHERE task_id=%s ORDER BY ordinal LIMIT 1)',
                    ('' if mode=='empty_input_version' else 'null',task))
        assert exact_failure.value.diag.message_primary=='immutable field artifacts.object_version'
        assert tenant_transaction_state(database,tenant)==snapshot
        assert database.execute('SELECT state,attempt,fencing_token FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('queued',0,old_fence)
        assert database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(task,)).fetchone()==(0,)
        return
    elif mode=='inputs':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=(SELECT artifact_id FROM photo.v2_task_inputs WHERE task_id=%s ORDER BY ordinal LIMIT 1)',(task,))
    else:
        database.execute("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,'{renderer_version}','\"v0\"') WHERE id=%s",(task,))
    suffix = ('_'+code if code else '') + '_STALE'
    with pytest.raises(errors.SerializationFailure,match='SEMANTIC_SHOWN_BACKGROUND_CLEANUP_CLAIM'+suffix):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_worker')
            database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command)))
    assert database.execute('SELECT state,attempt,fencing_token FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('queued',0,old_fence)
    assert database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(task,)).fetchone()==(0,)
    assert database.execute('SELECT fencing_token FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()==(old_fence,)
    assert database.execute('SELECT fencing_token FROM photo.resource_reservations WHERE task_id=%s',(task,)).fetchone()==(old_fence,)
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()==before


@pytest.mark.parametrize('mode,expected',[
    ('foreign_worker','unauthorized'),('changed_command','stale'),('wrong_attempt','stale'),
    ('cancel_task','cancelled'),('cancel_run','cancelled'),('expired_slot','unavailable'),
    ('expired_reservation','unavailable'),('unqualified_worker','unavailable'),
])
def test_background_cleanup_claim_refuses_before_start(database,mode,expected):
    from uuid import uuid4
    tenant,task,run,worker,command,old_fence = dispatched_copy_fixture(database)
    before = database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    attempt = 1
    if mode=='foreign_worker':
        worker = uuid4()
    elif mode=='changed_command':
        command = {**command,'operation':'semantic.adopt_shown_removal'}
    elif mode=='wrong_attempt':
        attempt = 2
    elif mode=='cancel_task':
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,))
    elif mode=='cancel_run':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
    elif mode=='expired_slot':
        database.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()-interval '1 second' WHERE task_id=%s",(task,))
    elif mode=='expired_reservation':
        database.execute("UPDATE photo.resource_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE task_id=%s",(task,))
    elif mode=='unqualified_worker':
        database.execute("UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE tenant_id=%s AND worker_id=%s",(tenant,worker))
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        assert database.execute('SELECT * FROM photo.claim_task(%s,%s,%s,%s)',(task,attempt,worker,Jsonb(command))).fetchone()[0]==expected
        database.execute('RESET ROLE')
    assert database.execute('SELECT state,attempt,fencing_token FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('queued',0,old_fence)
    assert database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(task,)).fetchone()==(0,)
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()==before


@pytest.mark.parametrize('mode',['stale_capacity','insufficient_ram','unqualified_worker','unavailable_input'])
def test_background_cleanup_scheduler_refusal_has_no_dispatch_side_effects(database,mode):
    dispatched_copy_fixture(database,refusal=mode)
