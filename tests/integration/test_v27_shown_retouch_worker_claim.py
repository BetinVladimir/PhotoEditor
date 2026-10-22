"""Actual own retouch Apply scheduler/claim SQL over immutable preview receipt.

Synthetic imported qualification and node capacities; no hardware, physical
storage, worker pixel execution or production capability enablement is claimed.
"""
from psycopg.types.json import Jsonb
import pytest
from photo_worker.v2.contract_lock import COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_PREVIOUS_SCHEMAS_SHA256
from retouch_copy_apply_fixture import committed_copy_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def dispatched_copy_fixture(db, refusal=None, *, include_documents=False,dense_warps=0,reference_dense_warps=None,with_alpha=False,schema_pin=None,historical_writer_head=None,forward_upgrade=False):
    """Run actual scheduler reservation before exercising actual worker claim."""
    from uuid import uuid4
    values, task, _ = committed_copy_fixture(db,dense_warps=dense_warps,reference_dense_warps=reference_dense_warps,with_alpha=with_alpha,historical_writer_head=historical_writer_head)
    tenant = values[0][0][0]
    task_id = task['node']['id']
    run, params, sha, fence, profile, inputs = db.execute(
        'SELECT t.run_id,t.params_artifact_id,a.sha256,t.fencing_token,t.execution_profile,t.inputs '
        'FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id '
        'WHERE t.id=%s', (task_id,)).fetchone()
    if historical_writer_head is not None:
        assert historical_writer_head in (2406, 2415)
        assert (historical_writer_head == 2406 and schema_pin == COPY_FENCE_PREVIOUS_SCHEMAS_SHA256) or (historical_writer_head == 2415 and (schema_pin is None or (forward_upgrade and schema_pin == '7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0')))
        # Actual 02407 commit writes the historical seal; never rewrite it.
        assert profile['contracts_sha'] == ('886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90' if historical_writer_head == 2406 else '7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0')
    if schema_pin is not None:
        # The historical case is constructed by the actual 02380 writer at
        # head2406. Never relabel an already sealed current task.
        assert profile['contracts_sha'] == schema_pin
        if forward_upgrade or schema_pin == COPY_FENCE_PREVIOUS_SCHEMAS_SHA256:
            assert historical_writer_head in (2406,2415)
            from pathlib import Path
            immutable = db.execute('SELECT params_artifact_id,input_digest,inputs,execution_profile FROM photo.tasks WHERE id=%s',(task_id,)).fetchone()
            for migration in sorted((Path(__file__).resolve().parents[2]/'backend/db/migrations').glob('*.sql')):
                if int(migration.name.split('_',1)[0]) > historical_writer_head:
                    db.execute(migration.read_text().split('-- +goose Down',1)[0])
            assert db.execute('SELECT params_artifact_id,input_digest,inputs,execution_profile FROM photo.tasks WHERE id=%s',(task_id,)).fetchone() == immutable
    worker = uuid4()
    node = 'retouch-claim-' + str(uuid4())
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,enabled,reported_at,doctor_report_sha256) VALUES(%s,%s,0,%s,%s,0,%s,true,clock_timestamp(),%s)',
        (node,2**40,2**40,2**40,2**40,'a'*64))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) "
        "VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',session_user,%s,true,'{\"semantic.adopt_shown_retouch_region\":[\"cpu\"]}','[\"v2.2\"]')",
        (tenant,worker,profile.get('model_lock_id','a'*64),node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'cpu1','cpu','{}')",(node,))
    # Isolated operator activation after the imported fixture qualification.
    # This is not measured hardware/model qualification or a production rollout.
    assert db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='semantic.adopt_shown_retouch_region' AND version='1' AND resource_class='cpu'").rowcount==1
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
    assert command['operation']=='semantic.adopt_shown_retouch_region'
    assert command['model'] is None and command['attempt']==1
    assert db.execute('SELECT dispatch_worker_id,dispatch_command FROM photo.tasks WHERE id=%s',(task_id,)).fetchone()==(worker,command)
    route,payload = db.execute('SELECT routing_key,payload FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()
    assert route=='worker.'+str(worker)+'.cpu.semantic_adopt_shown_retouch_region'
    assert payload==command
    assert db.execute('SELECT ram_bytes,vram_bytes,scratch_bytes,fencing_token FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',
        (tenant,task_id)).fetchone()==(estimate['ram_bytes'],0,estimate['scratch_bytes'],fence)
    result=(tenant,task_id,run,worker,command,fence)
    return (*result,values,task) if include_documents else result


@pytest.mark.parametrize("schema_pin", [COPY_FENCE_SCHEMAS_SHA256, pytest.param(COPY_FENCE_PREVIOUS_SCHEMAS_SHA256, marks=pytest.mark.migration_head(2406))])
def test_retouch_claim_starts_run_and_fences_duplicate(database,schema_pin):
    tenant,task,run,worker,command,old_fence = dispatched_copy_fixture(database,schema_pin=schema_pin,historical_writer_head=2406 if schema_pin == COPY_FENCE_PREVIOUS_SCHEMAS_SHA256 else None)
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


@pytest.mark.parametrize('mode',['stale_capacity','insufficient_ram','unqualified_worker','unavailable_input'])
def test_retouch_scheduler_refusal_has_no_dispatch_side_effects(database,mode):
    dispatched_copy_fixture(database,refusal=mode)


@pytest.mark.parametrize('mode,expected',[
    ('foreign_worker','unauthorized'),('changed_command','stale'),('wrong_attempt','stale'),
    ('cancel_task','cancelled'),('cancel_run','cancelled'),('stale_worker','unavailable'),
])
def test_retouch_claim_refuses_foreign_or_cancelled_scope_before_start(database,mode,expected):
    from uuid import uuid4
    tenant,task,run,worker,command,old_fence=dispatched_copy_fixture(database)
    before=database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    attempt=1
    if mode=='foreign_worker':
        worker=uuid4()
    elif mode=='changed_command':
        command={**command,'operation':'semantic.adopt_shown_removal'}
    elif mode=='wrong_attempt':
        attempt=2
    elif mode=='cancel_task':
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,))
    elif mode=='stale_worker':
        database.execute("UPDATE photo.worker_registrations SET last_seen=clock_timestamp()-interval '1 hour' WHERE tenant_id=%s AND worker_id=%s",(tenant,worker))
    else:
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        assert database.execute('SELECT * FROM photo.claim_task(%s,%s,%s,%s)',
            (task,attempt,worker,Jsonb(command))).fetchone()[0]==expected
        database.execute('RESET ROLE')
    assert database.execute('SELECT state,attempt,fencing_token FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('queued',0,old_fence)
    assert database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(task,)).fetchone()==(0,)
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()==before



@pytest.mark.parametrize('mode,code',[('renderer','WORKER'),('quota','QUOTA'),('inputs','INPUTS')])
def test_retouch_late_admission_loss_rolls_back_entire_base_claim(database,mode,code):
    from psycopg import errors
    tenant,task,run,worker,command,old_fence=dispatched_copy_fixture(database)
    before=database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    if mode=='renderer':
        database.execute("UPDATE photo.worker_registrations SET v2_renderer_versions='[]' WHERE tenant_id=%s AND worker_id=%s",(tenant,worker))
    elif mode=='quota':
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run))
    else:
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=(SELECT artifact_id FROM photo.v2_task_inputs WHERE task_id=%s ORDER BY ordinal LIMIT 1)',(task,))
    with pytest.raises(errors.SerializationFailure,match='SEMANTIC_SHOWN_RETOUCH_CLAIM_'+code+'_STALE'):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_worker')
            database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command)))
    assert database.execute('SELECT state,attempt,fencing_token,dispatch_worker_id,dispatch_command FROM photo.tasks WHERE id=%s',
        (task,)).fetchone()==('queued',0,old_fence,worker,command)
    assert database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(task,)).fetchone()==(0,)
    assert database.execute('SELECT fencing_token FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()==(old_fence,)
    assert database.execute('SELECT fencing_token FROM photo.resource_reservations WHERE task_id=%s',(task,)).fetchone()==(old_fence,)
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()==before
