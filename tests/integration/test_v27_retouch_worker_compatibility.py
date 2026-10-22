"""Deferred scheduler-to-claim compatibility tests on disposable databases.

Qualification metadata is synthetic; these scenarios do not qualify hardware.
"""
import pytest
from retouch_operator_fixture import retouch_preview_operator_enabled

pytestmark = pytest.mark.usefixtures('retouch_preview_operator_enabled')
from psycopg import errors, sql
from psycopg.types.json import Jsonb
from test_v27_retouch_preview_worker_claim import dispatched_preview_fixture
from test_v27_shown_retouch_worker_claim import dispatched_copy_fixture


@pytest.mark.parametrize('kind', ['preview', 'shown'])
@pytest.mark.parametrize('mutation', ['cuda', 'mps', 'resource', 'qualification', 'renderer', 'disabled'])
def test_worker_compatibility_loss_after_dispatch_preserves_claim_and_recovers(db, kind, mutation):
    fixture = dispatched_preview_fixture if kind == 'preview' else dispatched_copy_fixture
    tenant, task, run, worker, command, old_fence = fixture(db)
    def snapshot():
        return (
            db.execute('SELECT state,attempt,fencing_token,worker_id,dispatch_worker_id,dispatch_command,lease_until,deadline FROM photo.tasks WHERE id=%s', (task,)).fetchone(),
            db.execute('SELECT status,revision FROM photo.runs WHERE id=%s', (run,)).fetchone(),
            db.execute('SELECT fencing_token,lease_until,node_id FROM photo.resource_slots WHERE task_id=%s ORDER BY id', (task,)).fetchall(),
            db.execute('SELECT fencing_token,ram_bytes,vram_bytes,scratch_bytes FROM photo.resource_reservations WHERE task_id=%s ORDER BY id', (task,)).fetchall(),
            db.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s', (task,)).fetchone())
    before = snapshot()
    assignments = {
        'cuda': ('backends', Jsonb(['cuda'])),
        'mps': ('backends', Jsonb(['mps'])),
        'resource': ('resource_classes', Jsonb(['gpu'])),
        'qualification': ('qualified_operations', Jsonb({})),
        'renderer': ('v2_renderer_versions', Jsonb([])),
        'disabled': ('enabled', False),
    }
    column, value = assignments[mutation]
    with db.transaction(force_rollback=True):
        db.execute(sql.SQL('UPDATE photo.worker_registrations SET {}=%s WHERE tenant_id=%s AND worker_id=%s').format(sql.Identifier(column)), (value, tenant, worker))
        try:
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_worker')
                result = db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)', (task, worker, Jsonb(command))).fetchone()
                db.execute('RESET ROLE')
        except errors.SerializationFailure as error:
            expected = 'SEMANTIC_RETOUCH_PREVIEW_CLAIM_WORKER_STALE' if kind == 'preview' else 'SEMANTIC_SHOWN_RETOUCH_CLAIM_WORKER_STALE'
            assert expected in str(error)
        else:
            # The base worker guard may refuse before the operation-specific guard.
            assert result[0] in ('unavailable', 'unauthorized')
        assert snapshot() == before
    assert snapshot() == before
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        restored = db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)', (task, worker, Jsonb(command))).fetchone()
        db.execute('RESET ROLE')
    assert restored[0] == 'claimed' and restored[1] == old_fence + 1
    assert db.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s', (task,)).fetchone() == (1,)
