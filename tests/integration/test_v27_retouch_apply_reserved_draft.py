"""Own draft/quota transaction scenarios, with no activation or dispatch."""
import json
from copy import deepcopy
from uuid import uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb
from retouch_copy_apply_fixture import admission_task_fixture
from test_v27_retouch_apply_decision_gate import state
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def prepared(db, *, with_alpha=False):
    values, task = admission_task_fixture(db, with_alpha=with_alpha)
    data, request, artifact, body, child = values[:5]
    args = data[0]
    revision = int(json.loads(args[5])['workflow_revision'])
    return values, task, (*args[:4], revision, request, artifact, body,
        Jsonb(data[2]['review_documents']), child, Jsonb([task]))


def reserve(db, args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        batch = db.execute('SELECT photo.create_semantic_shown_retouch_apply_reserved_draft('
            + ','.join(['%s']*11) + ')', args).fetchone()[0]
        db.execute('RESET ROLE')
        return batch


@pytest.mark.parametrize('with_alpha', [False, True])
def test_reserved_draft_exact_quota_replay_without_activation(database, with_alpha):
    values, task, args = prepared(database, with_alpha=with_alpha)
    tenant, child, project = args[0], values[4], values[0][2]['project']
    amount = task['node']['execution_profile']['resource_estimate']['output_bytes']
    usage_before = database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    batch = reserve(database, args)
    assert database.execute('SELECT status,graph_sealed,cancel_requested FROM photo.runs '
        'WHERE tenant_id=%s AND id=%s', (tenant, child)).fetchone() == ('draft', False, False)
    assert database.execute('SELECT state,attempt,fencing_token FROM photo.tasks '
        'WHERE tenant_id=%s AND run_id=%s', (tenant, child)).fetchall() == [('blocked', 0, 0)]
    assert database.execute('SELECT batch_id,child_run_id,state,revision FROM photo.semantic_targets '
        'WHERE tenant_id=%s AND id=%s', (tenant, values[0][0][4])).fetchone() == (batch, child, 'queued', 2)
    reservation = database.execute('SELECT bytes,files,state,expires_at FROM photo.quota_reservations '
        "WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant, child)).fetchone()
    assert reservation[:3] == (amount, 0, 'reserved') and reservation[3] is not None
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()[0] == usage_before + amount
    before = state(database, tenant)
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_fn_owner')
        assert database.execute('SELECT photo.reserve_semantic_shown_retouch_apply_quota(%s,%s,%s,%s,%s)',
            (tenant, project, args[2], child, Jsonb([task]))).fetchone()[0] == amount
        database.execute('RESET ROLE')
    assert state(database, tenant) == before
    # The private draft constructor is conflict-only; public saved-receipt replay
    # belongs to the enclosing commit, never to a second shell construction.
    with pytest.raises(psycopg.Error) as error:
        reserve(database, args)
    assert error.value.sqlstate == '40001'
    assert state(database, tenant) == before


@pytest.mark.parametrize('with_alpha', [False, True])
def test_quota_failure_rolls_back_shell_tasks_batch_and_target(database, with_alpha):
    values, _, args = prepared(database, with_alpha=with_alpha)
    tenant, child = args[0], values[4]
    with database.transaction(force_rollback=True):
        database.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0'::jsonb) WHERE id=%s", (tenant,))
        before = state(database, tenant)
        with pytest.raises(psycopg.Error) as error:
            reserve(database, args)
        assert error.value.sqlstate == '23514'
        assert 'SEMANTIC_SHOWN_QUOTA_EXCEEDED' in str(error.value)
        assert state(database, tenant) == before
        assert database.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, child)).fetchone() == (0,)
    # Original documents and proposed target remain usable after rollback.
    assert reserve(database, args) is not None


@pytest.mark.parametrize('mutation', ['expired', 'bytes', 'files', 'quota_lowered'])
def test_existing_reservation_conflicts_do_not_renew_or_charge_again(database, mutation):
    values, task, args = prepared(database, with_alpha=True)
    tenant, child, project = args[0], values[4], values[0][2]['project']
    assert reserve(database, args) is not None
    with database.transaction(force_rollback=True):
        if mutation == 'quota_lowered':
            database.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0'::jsonb) WHERE id=%s", (tenant,))
        else:
            assignments = dict(expired="expires_at=clock_timestamp()-interval '1 second'",
                bytes='bytes=bytes+1', files='files=1')
            database.execute('UPDATE photo.quota_reservations SET ' + assignments[mutation]
                + " WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (tenant, child))
        before = state(database, tenant)
        with pytest.raises(psycopg.Error) as error:
            with database.transaction():
                database.execute('SET LOCAL ROLE photo_fn_owner')
                database.execute('SELECT photo.reserve_semantic_shown_retouch_apply_quota(%s,%s,%s,%s,%s)',
                    (tenant, project, args[2], child, Jsonb([task]))).fetchone()
        assert error.value.sqlstate == '23514'
        assert 'SEMANTIC_SHOWN_QUOTA_CONFLICT' in str(error.value)
        assert state(database, tenant) == before


@pytest.mark.parametrize('mutation', ['tenant', 'project', 'actor', 'child', 'task_identity'])
def test_reservation_replay_refuses_foreign_scope_and_changed_task(database, mutation):
    values, task, args = prepared(database, with_alpha=True)
    assert reserve(database, args) is not None
    call = [args[0], values[0][2]['project'], args[2], values[4], Jsonb([task])]
    if mutation == 'task_identity':
        changed = deepcopy(task)
        changed['node']['id'] = str(uuid4())
        call[4] = Jsonb([changed])
    else:
        call[('tenant', 'project', 'actor', 'child').index(mutation)] = uuid4()
    before = state(database, args[0])
    with pytest.raises(psycopg.Error) as error:
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_fn_owner')
            database.execute('SELECT photo.reserve_semantic_shown_retouch_apply_quota(%s,%s,%s,%s,%s)', call).fetchone()
    assert error.value.sqlstate == '23514'
    assert state(database, args[0]) == before
