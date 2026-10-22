"""Actual preview scheduler/claim SQL with synthetic qualified metadata.

Uses isolated database fixture. No worker compute, physical object storage,
photographic qualification or production enablement is claimed.
"""
from psycopg.types.json import Jsonb
import pytest
from retouch_operator_fixture import retouch_preview_operator_enabled

pytestmark = pytest.mark.usefixtures('retouch_preview_operator_enabled')
from photo_worker.v2.contract_lock import COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_PREVIOUS_SCHEMAS_SHA256
from retouch_preview_admission_fixture import prepared_retouch_preview_fixture, activate_retouch_preview_fixture
from conftest import database as isolated_database_config


@pytest.fixture
def database(isolated_database_config):
    """One real migrated private DB per SQL case, with owning rollback scope."""
    import psycopg
    with psycopg.connect(**isolated_database_config) as connection:
        with connection.transaction(force_rollback=True):
            yield connection


def _retouch_dispatch_prerequisite_facts(db, task):
    """Owner-only diagnostic SELECT; no principal, payload, locator or secrets printed.

    Mirror 02364's own gate without changing it. SQL NULL is intentionally retained
    for individual conjuncts; only the installed complete gate is normalized by SQL.
    Domain graph activation does not imply operator registry enablement.
    """
    row = db.execute("""
        SELECT jsonb_build_object(
          'task_shape', t.operation='semantic.preview_retouch_region'
            AND t.operation_version='1' AND t.resource_class='cpu',
          'task_state',t.state IN('ready','queued') AND NOT t.cancel_requested,
          'task_backend',t.execution_profile->>'backend'='cpu',
          'run_shape',r.kind='preview' AND r.status IN('queued','running')
            AND r.graph_sealed AND NOT r.cancel_requested,
          'intent_binding',i.id IS NOT NULL,
          'registry_enabled',EXISTS(SELECT 1 FROM photo.operation_registry op
            WHERE op.operation=t.operation AND op.version=t.operation_version
              AND op.resource_class='cpu' AND op.v2_enabled AND op.backends ? 'cpu'),
          'closure_ready',photo.semantic_retouch_preview_closure_ready(t.id),
          'run_current',photo.semantic_retouch_preview_run_current(t.tenant_id,r.id),
          'profile_ready',photo.v27_downstream_profile_ready(t.tenant_id,i.project_id,
            t.operation,t.operation_version,'cpu',r.v27_preapply_preview_profile),
          'quota_exact_live',EXISTS(SELECT 1 FROM photo.quota_reservations q
            WHERE q.tenant_id=t.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
              AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by
              AND q.files=0 AND q.share_id IS NULL
              AND q.bytes=(r.v27_preapply_preview_profile->'resource_estimate'->>'output_bytes')::bigint
              AND q.expires_at>clock_timestamp()),
          'installed_dispatch_ready',photo.semantic_retouch_preview_dispatch_ready(t.id))
        FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
        LEFT JOIN photo.semantic_intents i ON i.tenant_id=t.tenant_id
          AND i.id::text=r.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id'
        WHERE t.id=%s
    """, (task,)).fetchone()
    if row is None:
        return {'row_present': False}
    facts = {'row_present': True, **row[0]}
    assert all(value is None or isinstance(value, bool) for value in facts.values())
    return facts


def dispatched_preview_fixture(db, refusal=None, *, dense_warps=0,reference_dense_warps=None,with_alpha=False,prepared=None,objects=None,schema_pin=None):
    """Run actual scheduler reservation before exercising actual worker claim."""
    from uuid import uuid4
    if prepared is not None and (objects is not None or with_alpha or dense_warps or reference_dense_warps is not None):
        raise ValueError("Prepared preview already owns its source and publication options")
    data = prepared if prepared is not None else prepared_retouch_preview_fixture(
        db, with_alpha=with_alpha, dense_warps=dense_warps,
        reference_dense_warps=reference_dense_warps, objects=objects)
    activate_retouch_preview_fixture(db, data, historical_schema_pin=schema_pin)
    tenant = data['args'][0]
    task_id = data['task']
    run, params, sha, fence, profile, inputs = db.execute(
        'SELECT t.run_id,t.params_artifact_id,a.sha256,t.fencing_token,t.execution_profile,t.inputs '
        'FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id '
        'WHERE t.id=%s', (task_id,)).fetchone()
    if schema_pin is not None:
        assert profile['contracts_sha'] == schema_pin
    worker = uuid4()
    node = 'retouch-claim-' + str(uuid4())
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,enabled,reported_at,doctor_report_sha256) VALUES(%s,%s,0,%s,%s,0,%s,true,clock_timestamp(),%s)',
        (node,2**40,2**40,2**40,2**40,'a'*64))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) "
        "VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,clock_timestamp(),'{}',session_user,%s,true,'{\"semantic.preview_retouch_region\":[\"cpu\"]}','[\"v2.2\"]')",
        (tenant,worker,profile.get('model_lock_id','a'*64),node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'cpu1','cpu','{}')",(node,))
    estimate = profile['resource_estimate']
    event, correlation = uuid4(), uuid4()
    expected_refusals = dict(stale_capacity='waiting_resource',insufficient_ram='waiting_resource',
        unqualified_worker='waiting_worker',unavailable_input='stale',operation_disabled='stale')
    if refusal=='stale_capacity':
        db.execute("UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '1 hour' WHERE node_id=%s",(node,))
    elif refusal=='insufficient_ram':
        db.execute('UPDATE photo.resource_nodes SET ram_available_bytes=1 WHERE node_id=%s',(node,))
    elif refusal=='unqualified_worker':
        db.execute("UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE tenant_id=%s AND worker_id=%s",(tenant,worker))
    elif refusal=='operation_disabled':
        db.execute("UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='semantic.preview_retouch_region' AND version='1'")
    elif refusal=='unavailable_input':
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(inputs[0]['artifact_id'],))
    elif refusal is not None:
        raise AssertionError('Unknown dispatch refusal')
    # Diagnostic is opt-in, before the original scheduler call/assert.
    import os
    if os.environ.get('PHOTO_TEST_RETOUCH_DISPATCH_DIAGNOSTIC') == '1':
        import json
        print('RETOUCH_DISPATCH_PREREQUISITES ' + json.dumps(
            _retouch_dispatch_prerequisite_facts(db, task_id), sort_keys=True), flush=True)
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
    assert command['operation']=='semantic.preview_retouch_region'
    assert command['model'] is None and command['attempt']==1
    assert db.execute('SELECT dispatch_worker_id,dispatch_command FROM photo.tasks WHERE id=%s',(task_id,)).fetchone()==(worker,command)
    route,payload = db.execute('SELECT routing_key,payload FROM photo.outbox WHERE tenant_id=%s AND event_id=%s',(tenant,event)).fetchone()
    assert route=='worker.'+str(worker)+'.cpu.semantic_preview_retouch_region'
    assert payload==command
    assert db.execute('SELECT ram_bytes,vram_bytes,scratch_bytes,fencing_token FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',
        (tenant,task_id)).fetchone()==(estimate['ram_bytes'],0,estimate['scratch_bytes'],fence)
    result=(tenant,task_id,run,worker,command,fence)
    return result


@pytest.mark.parametrize("schema_pin", [COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_PREVIOUS_SCHEMAS_SHA256])
def test_preview_claim_starts_run_and_fences_duplicate(database,schema_pin):
    tenant,task,run,worker,command,old_fence = dispatched_preview_fixture(database,schema_pin=schema_pin)
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



@pytest.mark.parametrize('mode', ['stale_capacity', 'insufficient_ram', 'unqualified_worker', 'unavailable_input', 'operation_disabled'])
def test_preview_dispatch_refusal_preserves_task_reservation_and_outbox(database, mode):
    dispatched_preview_fixture(database, refusal=mode)


def claimed_preview_admission_fixture(db):
    from photo_worker.contracts.base import canonical_bytes
    tenant, task, run, worker, command, _ = dispatched_preview_fixture(db)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        claim = db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',
            (task, worker, Jsonb(command))).fetchone()
        assert claim[0] == 'claimed'
        db.execute('RESET ROLE')
    digest, config = db.execute('SELECT t.input_digest,r.config_snapshot FROM photo.tasks t '
        'JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=%s',
        (task,)).fetchone()
    raw = canonical_bytes(config['semantic_retouch_preview']['params'])
    return [tenant, run, task, 1, claim[1], worker, command['params']['artifact_id'],
        command['params']['sha256'], raw.decode(), digest]


def admit_preview(db, scope):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        value = db.execute('SELECT photo.admit_semantic_retouch_preview('+','.join(['%s']*10)+')',
            scope).fetchone()[0]
        db.execute('RESET ROLE')
        return value


@pytest.mark.parametrize('mutation', [None, 'quota', 'task_cancel', 'run_cancel',
    'lease', 'slot_lease', 'fence', 'digest', 'params_sha', 'policy'])
def test_running_preview_admission_repeats_live_authority_without_mutating_task(database, mutation):
    scope = claimed_preview_admission_fixture(database)
    tenant, run, task = scope[:3]
    assert admit_preview(database, scope) is True
    if mutation == 'quota':
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant, run))
    elif mutation == 'task_cancel':
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s', (task,))
    elif mutation == 'run_cancel':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (run,))
    elif mutation == 'lease':
        database.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s", (task,))
    elif mutation == 'slot_lease':
        database.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()-interval '1 second' WHERE task_id=%s", (task,))
    elif mutation == 'fence': scope[4] += 1
    elif mutation == 'digest': scope[9] = '0' * 64
    elif mutation == 'params_sha': scope[7] = '0' * 64
    elif mutation == 'policy':
        database.execute("UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=(SELECT (execution_profile->'qualification'->>'policy_id')::uuid FROM photo.tasks WHERE id=%s)", (task,))
    before = database.execute('SELECT state,attempt,fencing_token,worker_id,lease_until,deadline,cancel_requested FROM photo.tasks WHERE id=%s', (task,)).fetchone()
    assert admit_preview(database, scope) is (mutation is None)
    assert database.execute('SELECT state,attempt,fencing_token,worker_id,lease_until,deadline,cancel_requested FROM photo.tasks WHERE id=%s', (task,)).fetchone() == before


@pytest.mark.parametrize('with_alpha', [False, True])
def test_preview_renderer_revocation_after_dispatch_rolls_back_claim_and_allows_retry(database, with_alpha):
    from psycopg import errors
    tenant, task, run, worker, command, old_fence = dispatched_preview_fixture(database, with_alpha=with_alpha)
    database.execute("UPDATE photo.worker_registrations SET v2_renderer_versions='[]' WHERE tenant_id=%s AND worker_id=%s", (tenant, worker))
    def snapshot():
        return (
            database.execute('SELECT state,attempt,fencing_token,worker_id,dispatch_worker_id,dispatch_command,lease_until,deadline FROM photo.tasks WHERE id=%s', (task,)).fetchone(),
            database.execute('SELECT status,revision FROM photo.runs WHERE id=%s', (run,)).fetchone(),
            database.execute('SELECT fencing_token,lease_until,node_id FROM photo.resource_slots WHERE task_id=%s ORDER BY id', (task,)).fetchall(),
            database.execute('SELECT fencing_token,ram_bytes,vram_bytes,scratch_bytes FROM photo.resource_reservations WHERE task_id=%s ORDER BY id', (task,)).fetchall())
    before = snapshot()
    with pytest.raises(errors.SerializationFailure, match='SEMANTIC_RETOUCH_PREVIEW_CLAIM_WORKER_STALE'):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_worker')
            database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)', (task, worker, Jsonb(command)))
    assert snapshot() == before
    assert database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s', (task,)).fetchone() == (0,)
    database.execute("UPDATE photo.worker_registrations SET v2_renderer_versions=%s WHERE tenant_id=%s AND worker_id=%s", (Jsonb(["v2.2"]), tenant, worker))
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        claimed = database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)', (task, worker, Jsonb(command))).fetchone()
        assert claimed[0] == 'claimed' and claimed[1] == old_fence + 1
        database.execute('RESET ROLE')


def test_preview_registry_withdrawal_after_dispatch_preserves_claim_and_recovers(database):
    from psycopg import errors
    tenant, task, run, worker, command, old_fence = dispatched_preview_fixture(database)
    def snapshot():
        return (database.execute('SELECT state,attempt,fencing_token,worker_id,lease_until,deadline FROM photo.tasks WHERE id=%s',(task,)).fetchone(),
                database.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(task,)).fetchone(),
                database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone())
    before = snapshot()
    with database.transaction(force_rollback=True):
        database.execute("UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='semantic.preview_retouch_region' AND version='1'")
        assert database.execute('SELECT photo.semantic_retouch_preview_dispatch_ready(%s)',(task,)).fetchone() == (False,)
        with pytest.raises(errors.SerializationFailure, match='V2_OPERATION_CLAIM_ACTIVATION_STALE'):
            with database.transaction():
                database.execute('SET LOCAL ROLE photo_worker')
                database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()
        assert snapshot() == before
    assert snapshot() == before
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        restored = database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()
        assert restored[0] == 'claimed' and restored[1] == old_fence + 1
        database.execute('RESET ROLE')
