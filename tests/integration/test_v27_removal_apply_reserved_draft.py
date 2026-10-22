"""Own draft/quota transaction scenarios, with no activation or dispatch."""
import json
from copy import deepcopy
from uuid import uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb
from removal_apply_documents_fixture import admission_task_fixture


def state(db, tenant):
    # Complete tenant rows, not counts: refusal must preserve existing content.
    tables = ('runs', 'tasks', 'semantic_targets', 'v2_review_batches',
              'semantic_intents', 'quota_reservations', 'tenant_usage',
              'idempotency_records', 'artifacts')
    return tuple(db.execute('SELECT to_jsonb(r) FROM photo.' + table
        + ' r WHERE tenant_id=%s ORDER BY to_jsonb(r)::text', (tenant,)).fetchall()
        for table in tables)


def prepared(db):
    values, task = admission_task_fixture(db)
    data, request, artifact, body, child = values[:5]
    args = data[0]
    revision = int(json.loads(args[5])['workflow_revision'])
    return values, task, (*args[:4], revision, request, artifact, body,
        Jsonb(data[2]['review_documents']), child, Jsonb([task]))


def reserve(db, args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        batch = db.execute('SELECT photo.create_semantic_shown_removal_apply_reserved_draft('
            + ','.join(['%s']*11) + ')', args).fetchone()[0]
        db.execute('RESET ROLE')
        return batch


def test_reserved_draft_exact_quota_replay_without_activation(db):
    values, task, args = prepared(db)
    tenant, child, project = args[0], values[4], values[0][2]['project']
    amount = task['node']['execution_profile']['resource_estimate']['output_bytes']
    usage_before = db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    batch = reserve(db, args)
    assert db.execute('SELECT status,graph_sealed,cancel_requested FROM photo.runs '
        'WHERE tenant_id=%s AND id=%s', (tenant, child)).fetchone() == ('draft', False, False)
    assert db.execute('SELECT state,attempt,fencing_token FROM photo.tasks '
        'WHERE tenant_id=%s AND run_id=%s', (tenant, child)).fetchall() == [('blocked', 0, 0)]
    assert db.execute('SELECT batch_id,child_run_id,state,revision FROM photo.semantic_targets '
        'WHERE tenant_id=%s AND id=%s', (tenant, values[0][0][4])).fetchone() == (batch, child, 'queued', 2)
    reservation = db.execute('SELECT bytes,files,state,expires_at FROM photo.quota_reservations '
        "WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant, child)).fetchone()
    assert reservation[:3] == (amount, 0, 'reserved') and reservation[3] is not None
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()[0] == usage_before + amount
    before = state(db, tenant)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT photo.reserve_semantic_shown_removal_apply_quota(%s,%s,%s,%s,%s)',
            (tenant, project, args[2], child, Jsonb([task]))).fetchone()[0] == amount
        db.execute('RESET ROLE')
    assert state(db, tenant) == before
    # The private draft constructor is conflict-only; public saved-receipt replay
    # belongs to the enclosing commit, never to a second shell construction.
    with pytest.raises(psycopg.Error) as error:
        reserve(db, args)
    assert error.value.sqlstate == '40001'
    assert state(db, tenant) == before


def test_quota_failure_rolls_back_shell_tasks_batch_and_target(db):
    values, _, args = prepared(db)
    tenant, child = args[0], values[4]
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0'::jsonb) WHERE id=%s", (tenant,))
        before = state(db, tenant)
        with pytest.raises(psycopg.Error) as error:
            reserve(db, args)
        assert error.value.sqlstate == '23514'
        assert 'SEMANTIC_SHOWN_QUOTA_EXCEEDED' in str(error.value)
        assert state(db, tenant) == before
        assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, child)).fetchone() == (0,)
    # Original documents and proposed target remain usable after rollback.
    assert reserve(db, args) is not None


@pytest.mark.parametrize('mutation', ['expired', 'bytes', 'files', 'quota_lowered'])
def test_existing_reservation_conflicts_do_not_renew_or_charge_again(db, mutation):
    values, task, args = prepared(db)
    tenant, child, project = args[0], values[4], values[0][2]['project']
    assert reserve(db, args) is not None
    with db.transaction(force_rollback=True):
        if mutation == 'quota_lowered':
            db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0'::jsonb) WHERE id=%s", (tenant,))
        else:
            assignments = dict(expired="expires_at=clock_timestamp()-interval '1 second'",
                bytes='bytes=bytes+1', files='files=1')
            db.execute('UPDATE photo.quota_reservations SET ' + assignments[mutation]
                + " WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant, child))
        before = state(db, tenant)
        with pytest.raises(psycopg.Error) as error:
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_fn_owner')
                db.execute('SELECT photo.reserve_semantic_shown_removal_apply_quota(%s,%s,%s,%s,%s)',
                    (tenant, project, args[2], child, Jsonb([task]))).fetchone()
        assert error.value.sqlstate == '23514'
        assert 'SEMANTIC_SHOWN_QUOTA_CONFLICT' in str(error.value)
        assert state(db, tenant) == before


@pytest.mark.parametrize('mutation', ['tenant', 'project', 'actor', 'child', 'task_identity'])
def test_reservation_replay_refuses_foreign_scope_and_changed_task(db, mutation):
    values, task, args = prepared(db)
    assert reserve(db, args) is not None
    call = [args[0], values[0][2]['project'], args[2], values[4], Jsonb([task])]
    if mutation == 'task_identity':
        changed = deepcopy(task)
        changed['node']['id'] = str(uuid4())
        call[4] = Jsonb([changed])
    else:
        call[('tenant', 'project', 'actor', 'child').index(mutation)] = uuid4()
    before = state(db, args[0])
    with pytest.raises(psycopg.Error) as error:
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('SELECT photo.reserve_semantic_shown_removal_apply_quota(%s,%s,%s,%s,%s)', call).fetchone()
    assert error.value.sqlstate == '23514'
    assert state(db, args[0]) == before


@pytest.mark.parametrize('signature', [
    'photo.create_semantic_shown_removal_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)',
    'photo.create_semantic_shown_removal_apply_reserved_draft(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)',
    'photo.reserve_semantic_shown_removal_apply_quota(uuid,uuid,uuid,uuid,jsonb)',
])
def test_removal_draft_components_are_private_definers(db, signature):
    owner, definer, config, public_execute = db.execute(
        "SELECT pg_get_userbyid(proowner),prosecdef,proconfig,EXISTS("
        "SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) "
        "WHERE grantee=0 AND privilege_type='EXECUTE') "
        'FROM pg_proc WHERE oid=to_regprocedure(%s)', (signature,),
    ).fetchone()
    assert owner == 'photo_fn_owner' and definer
    assert 'search_path=pg_catalog, photo' in config
    assert not public_execute
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',
                              (role, signature)).fetchone()[0]


@pytest.mark.parametrize('mutation', [
    'empty', 'duplicate', 'target', 'task', 'operation', 'resource_class',
    'params_sha', 'params_body', 'input_digest', 'backend', 'vram',
    'output_bytes', 'qualification', 'transport', 'inventory',
    'image_bytes', 'presentation_bytes', 'protection_bytes',
])
def test_removal_reserved_draft_refuses_changed_batch_without_partial_writes(db, mutation):
    values, task, args = prepared(db)
    changed = deepcopy(task)
    tasks = [changed]
    if mutation == 'empty':
        tasks = []
    elif mutation == 'duplicate':
        tasks.append(deepcopy(changed))
    elif mutation == 'target':
        changed['target_id'] = str(uuid4())
    elif mutation == 'task':
        changed['node']['id'] = str(uuid4())
    elif mutation == 'operation':
        changed['node']['operation'] = 'semantic.preview_remove_object'
    elif mutation == 'resource_class':
        changed['node']['resource_class'] = 'gpu'
    elif mutation == 'params_sha':
        changed['params_sha'] = '0' * 64
    elif mutation == 'params_body':
        changed['params_body'] += ' '
    elif mutation == 'input_digest':
        changed['node']['input_digest'] = '0' * 64
    elif mutation == 'backend':
        changed['node']['execution_profile']['backend'] = 'cuda'
    elif mutation == 'vram':
        changed['node']['execution_profile']['resource_estimate']['vram_bytes'] = 1
    elif mutation == 'output_bytes':
        changed['node']['execution_profile']['resource_estimate']['output_bytes'] = 1
    elif mutation == 'qualification':
        changed['node']['execution_profile']['qualification']['qualification_id'] = str(uuid4())
    elif mutation == 'transport':
        changed['transport'] = []
    elif mutation == 'inventory':
        changed['inventory'] = []
    else:
        key = {'image_bytes': 'image', 'presentation_bytes': 'presentation',
               'protection_bytes': 'protection'}[mutation]
        changed['documents'][key] += ' '
    call = list(args)
    call[-1] = Jsonb(tasks)
    before = state(db, args[0])
    with pytest.raises(psycopg.Error) as failure:
        reserve(db, tuple(call))
    assert failure.value.sqlstate in ('23514', '22023'), failure.value
    assert 'SEMANTIC_' in str(failure.value)
    assert state(db, args[0]) == before
    # Refusal did not consume the valid published selection or draft identity.
    assert reserve(db, args) is not None


@pytest.mark.parametrize('mode', ['valid', 'reverse', 'second_image', 'second_profile', 'quota'])
def test_removal_multi_task_draft_reserves_total_or_rolls_back_every_target(db, mode):
    from removal_multi_selection_fixture import multi_apply_task_fixture
    bundles, gate, child, tasks = multi_apply_task_fixture(db)
    valid_tasks = deepcopy(tasks)
    total = sum(task['node']['execution_profile']['resource_estimate']['output_bytes'] for task in tasks)
    if mode == 'reverse':
        tasks.reverse()
    elif mode == 'second_image':
        tasks[1]['documents']['image'] += ' '
    elif mode == 'second_profile':
        tasks[1]['node']['execution_profile']['resource_estimate']['output_bytes'] = 1
    elif mode == 'quota':
        db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0'::jsonb) WHERE id=%s", (gate[0],))
    call = (*gate[:8], Jsonb(gate[8]), child, Jsonb(tasks))
    before = state(db, gate[0])
    if mode in ('valid', 'reverse'):
        batch = reserve(db, call)
        assert batch is not None
        assert db.execute('SELECT count(*) FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND state=\'blocked\'', (gate[0], child)).fetchone() == (2,)
        assert db.execute('SELECT bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type=\'run\' AND holder_id=%s', (gate[0], child)).fetchone() == (total,)
        for bundle in bundles:
            assert db.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s', (gate[0], bundle['target'])).fetchone() == ('queued', child, batch)
    else:
        with pytest.raises(psycopg.Error) as failure:
            reserve(db, call)
        assert failure.value.sqlstate == '23514', failure.value
        assert state(db, gate[0]) == before
        assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s', (gate[0], child)).fetchone() == (0,)
        if mode != 'quota':
            assert reserve(db, (*gate[:8], Jsonb(gate[8]), child, Jsonb(valid_tasks))) is not None
