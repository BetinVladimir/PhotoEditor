"""Worker argument binding for the independently scoped V28 SQL gate."""

from types import SimpleNamespace
from uuid import uuid4

from photo_worker.runtime.database import TaskDatabase


def test_restoration_gate_binds_exact_leased_attempt_and_params():
    ids = [uuid4() for _ in range(5)]
    params = SimpleNamespace(artifact_id=ids[4], sha256='a' * 64)
    command = SimpleNamespace(tenant_id=ids[0], run_id=ids[1], task_id=ids[2],
                              attempt=4, params=params)
    lease = SimpleNamespace(command=command, fence=19, worker_id=ids[3],
                            input_digest='b' * 64)
    calls = []
    db = object.__new__(TaskDatabase)
    db._query = lambda query, args: (calls.append((query, args)) or (True,))

    assert db.restoration_lease_ready(lease, b'{"schema_version":1}') is True
    assert 'photo.v28_restoration_lease_ready' in calls[0][0]
    assert calls[0][1] == (*ids[:3], 4, 19, ids[3], ids[4], 'a' * 64,
                            '{"schema_version":1}', 'b' * 64)


def test_qualification_gate_binds_the_same_attempt_and_denies_absent_row():
    ids = [uuid4() for _ in range(5)]
    params = SimpleNamespace(artifact_id=ids[4], sha256='a' * 64)
    command = SimpleNamespace(tenant_id=ids[0], run_id=ids[1], task_id=ids[2],
                              attempt=4, params=params)
    lease = SimpleNamespace(command=command, fence=19, worker_id=ids[3],
                            input_digest='b' * 64)
    calls = []
    db = object.__new__(TaskDatabase)
    db._query = lambda query, args: (calls.append((query, args)) or None)

    assert db.restoration_qualification_ready(lease, b'{"schema_version":1}') is False
    assert 'photo.v28_restoration_qualification_ready' in calls[0][0]
    assert calls[0][1] == (*ids[:3], 4, 19, ids[3], ids[4], 'a' * 64,
                            '{"schema_version":1}', 'b' * 64)
