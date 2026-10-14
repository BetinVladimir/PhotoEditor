"""Worker-only SQL grant read preserves the current import lease and scope."""
from types import SimpleNamespace
from uuid import uuid4
import pytest

from photo_worker.runtime.database import TaskDatabase


@pytest.mark.parametrize('row', [None, (None,), (4,)])
def test_import_person_mask_database_preserves_exact_lease_scope(row):
    command = SimpleNamespace(tenant_id=uuid4(), run_id=uuid4(), task_id=uuid4(),
        attempt=3, params=SimpleNamespace(artifact_id=uuid4(), sha256='a'*64))
    lease = SimpleNamespace(command=command, fence=17, worker_id=uuid4(), input_digest='b'*64)
    workflow, project, version, mask = uuid4(), uuid4(), uuid4(), uuid4()
    database = TaskDatabase('unused')
    calls = []
    database._query = lambda sql, args: (calls.append((sql, args)), row)[1]
    result = database.interchange_import_person_mask_live(
        lease, b'{}', workflow, project, version, mask, 'c'*64)
    assert result == (None if row is None else row[0])
    assert calls == [(
        'SELECT photo.v30_import_worker_person_mask_live(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
        (command.tenant_id, command.run_id, command.task_id, command.attempt,
         lease.fence, lease.worker_id, command.params.artifact_id,
         command.params.sha256, '{}', lease.input_digest,
         workflow, project, version, mask, 'c'*64))]


def test_import_person_mask_readiness_requires_worker_sql_function():
    database = TaskDatabase('unused')
    observed = []
    database._worker_function_set_ready = lambda signatures: (observed.extend(signatures), True)[1]
    assert database.interchange_import_person_mask_ready() is True
    assert observed == [
        'photo.v30_import_worker_person_mask_live(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,uuid,uuid,uuid,text)']
