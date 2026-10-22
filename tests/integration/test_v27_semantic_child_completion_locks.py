"""Caller transaction lock lifetime for accepted shown child completion.

Disposable metadata fixtures; no execution or physical processing qualification.
"""
from uuid import UUID
import psycopg
import pytest
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture, adopt
from test_v27_semantic_child_canonical_completion import finalize


@pytest.mark.parametrize('kind', ['tenant', 'run', 'task', 'target', 'quota', 'usage'])
def test_child_completion_retains_existing_rows_until_caller_transaction_ends(database, kind):
    with psycopg.connect(**database) as seed:
        values, task, commit = successful_copy_metadata_fixture(seed)
        tenant, task_id, child = commit[0], commit[1], values[4]
        assert adopt(seed, commit) is True
        target = seed.execute('SELECT id FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',
                              (tenant, child)).fetchone()[0]
        quota = seed.execute("SELECT id FROM photo.quota_reservations WHERE tenant_id=%s "
                             "AND holder_type='run' AND holder_id=%s", (tenant, child)).fetchone()[0]
    # Explicit static identifiers; no caller-provided table or column names.
    rows = dict(tenant=('tenants', 'id', tenant), run=('runs', 'id', child),
                task=('tasks', 'id', task_id), target=('semantic_targets', 'id', target),
                quota=('quota_reservations', 'id', quota), usage=('tenant_usage', 'tenant_id', tenant))
    table, column, identifier = rows[kind]
    query = f'SELECT {column} FROM photo.{table} WHERE {column}=%s FOR UPDATE NOWAIT'
    with psycopg.connect(**database) as holder, psycopg.connect(**database) as writer:
        holder.execute('BEGIN')
        assert finalize(holder, tenant, child) == 'completed'
        with pytest.raises(psycopg.errors.LockNotAvailable):
            with writer.transaction():
                writer.execute(query, (identifier,)).fetchone()
        holder.rollback()
        with writer.transaction(force_rollback=True):
            assert writer.execute(query, (identifier,)).fetchone() == (UUID(str(identifier)),)
        # Full rollback restores the admitted child and permits a clean retry.
        assert finalize(holder, tenant, child) == 'completed'
