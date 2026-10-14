from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4, uuid5
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.interchange_plan import InterchangeExportParams
from photo_worker.operations.interchange.export_admission import SQLExportAdmission
from photo_worker.runtime.executor import OperationFailure


def fixture():
    task = uuid4()
    params = InterchangeExportParams(schema_version=1,
        plan=ArtifactRef(artifact_id=uuid4(), sha256='a'*64), max_report_bytes=8388608)
    command = SimpleNamespace(operation='v2.interchange.export', operation_version='1', resource_class='cpu',
        task_id=task, params=ArtifactRef(artifact_id=uuid5(task, 'interchange:params:1'),
                                       sha256=sha256(canonical_bytes(params)).hexdigest()))
    ctx = SimpleNamespace(check_active=lambda: None, lease=SimpleNamespace(command=command))
    response = dict(workflow_id=str(uuid4()), project_id=str(uuid4()), snapshot_digest='b'*64,
                    max_archive_bytes=1024, max_output_bytes=2048, max_input_json_bytes=4096)
    class Database:
        calls = 0
        def interchange_export_context(self, lease, raw):
            self.calls += 1
            assert raw == canonical_bytes(params)
            return response
    return ctx, params, response, Database()


def test_sql_export_admission_returns_exact_immutable_authority():
    ctx, params, response, database = fixture()
    authority = SQLExportAdmission(database).admit(ctx, params)
    assert str(authority.workflow_id) == response['workflow_id']
    assert authority.max_output_bytes == 2048
    response['max_output_bytes'] = 9999
    assert authority.max_output_bytes == 2048
    assert database.calls == 1


@pytest.mark.parametrize('field,value', [('workflow_id', str(uuid5(uuid4(), 'unused'))),
    ('project_id', 'invalid'), ('snapshot_digest', 'x'*64), ('max_archive_bytes', True),
    ('max_output_bytes', 1023), ('max_input_json_bytes', 0), ('unknown', 1)])
def test_sql_export_admission_rejects_invalid_sql_receipt(field, value):
    ctx, params, response, database = fixture()
    if field == 'workflow_id': value = '00000000-0000-0000-0000-000000000000'
    response[field] = value
    with pytest.raises(OperationFailure, match='SOURCE_STALE'):
        SQLExportAdmission(database).admit(ctx, params)


@pytest.mark.parametrize('mode', ['params_hash', 'params_identity', 'operation'])
def test_sql_export_admission_rejects_wrong_task_before_database(mode):
    ctx, params, _, database = fixture()
    if mode == 'params_hash': ctx.lease.command.params.sha256 = '0'*64
    elif mode == 'params_identity': ctx.lease.command.params.artifact_id = uuid4()
    else: ctx.lease.command.operation = 'v2.interchange.import'
    with pytest.raises(OperationFailure, match='SOURCE_STALE'):
        SQLExportAdmission(database).admit(ctx, params)
    assert database.calls == 0


def test_sql_export_transport_preserves_complete_attempt_identity():
    from photo_worker.runtime.database import TaskDatabase
    ctx, params, _, _ = fixture()
    command = ctx.lease.command
    command.tenant_id, command.run_id, command.attempt = uuid4(), uuid4(), 3
    ctx.lease.fence, ctx.lease.worker_id, ctx.lease.input_digest = 17, uuid4(), 'c'*64
    captured = []
    database = TaskDatabase.__new__(TaskDatabase)
    database._query = lambda sql, values: captured.append((sql, values)) or ({'receipt': 'test'},)
    raw = canonical_bytes(params)
    assert database.interchange_export_context(ctx.lease, raw) == {'receipt': 'test'}
    sql, values = captured[0]
    assert 'photo.v30_interchange_export_ready' in sql
    assert values == (command.tenant_id, command.run_id, command.task_id, 3, 17,
        ctx.lease.worker_id, command.params.artifact_id, command.params.sha256,
        raw.decode('utf-8'), ctx.lease.input_digest)
