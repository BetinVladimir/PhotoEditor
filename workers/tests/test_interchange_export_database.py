"""Exact worker lease argument routing for export authority SQL bridges."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime.database import TaskDatabase


def lease():
    command = SimpleNamespace(tenant_id=uuid4(), run_id=uuid4(), task_id=uuid4(), attempt=3,
        params=SimpleNamespace(artifact_id=uuid4(), sha256='a' * 64))
    return SimpleNamespace(command=command, fence=17, worker_id=uuid4(), input_digest='b' * 64)


def arguments(value):
    command = value.command
    return (command.tenant_id, command.run_id, command.task_id, command.attempt,
        value.fence, value.worker_id, command.params.artifact_id, command.params.sha256,
        '{}', value.input_digest)


@pytest.mark.parametrize('row', [None, (None,), ({'artifact_id':str(uuid4()),'sha256':'c'*64},)])
def test_export_snapshot_database_preserves_exact_lease_and_absence(row):
    database = TaskDatabase('unused'); value = lease(); calls = []
    database._query = lambda sql, args: (calls.append((sql,args)), row)[1]
    assert database.interchange_export_snapshot(value, b'{}') == (None if row is None else row[0])
    assert calls == [('SELECT photo.v30_export_snapshot_authority(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', arguments(value))]


@pytest.mark.parametrize('row', [None, (None,), (False,), (1,), (True,)])
def test_export_descriptor_database_requires_boolean_and_preserves_evidence(row):
    database = TaskDatabase('unused'); value = lease(); calls = []; evidence = {'contract':'WorkingPixels'}
    database._query = lambda sql, args: (calls.append((sql,args)), row)[1]
    assert database.interchange_export_descriptor_authority(value, b'{}', evidence) is (row is not None and row[0] is True)
    sql, args = calls[0]
    assert sql == 'SELECT photo.v30_export_descriptor_authority(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
    assert args[:-1] == arguments(value)
    assert args[-1].obj == evidence
