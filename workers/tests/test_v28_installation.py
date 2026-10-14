"""V28 registration is opt-in and binds startup admission, never task Params."""

from contextlib import nullcontext
from types import SimpleNamespace
from unittest.mock import Mock
from uuid import uuid4

import pytest

from photo_worker.runtime.service import WorkerConfigurationError
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.v28_installation import installed_v28_operations
from photo_worker.runtime.database import TaskDatabase


def config(backend='cpu', resource_class='gpu'):
    return SimpleNamespace(backend=backend, resource_class=resource_class,
                           max_scratch_bytes=1024)


def database(ready=True, qualified=True):
    return SimpleNamespace(restoration_lease_ready=lambda *_: ready,
                           restoration_qualification_ready=lambda *_: qualified,
                           glare_donor_source_ready=lambda *_: ready)


def test_no_installation_leaves_both_operations_unregistered():
    assert installed_v28_operations(object(), object(), config()) == []


def test_restoration_sql_gates_require_worker_only_owner_functions(monkeypatch):
    db = TaskDatabase('unused')
    ready = ('photo_fn_owner', True, 'search_path=pg_catalog, photo', True, False, False)
    query = Mock(side_effect=[ready, ready])
    monkeypatch.setattr(db, '_query', query)
    assert db.restoration_runtime_ready() is True
    assert query.call_count == 2
    query = Mock(side_effect=[ready, (*ready[:4], True, False)])
    monkeypatch.setattr(db, '_query', query)
    assert db.restoration_runtime_ready() is False
    monkeypatch.setattr(db, '_query', Mock(return_value=None))
    assert db.restoration_runtime_ready() is False


def test_glare_donor_sql_gate_requires_worker_only_owner(monkeypatch):
    db = TaskDatabase('unused')
    ready = ('photo_fn_owner', True, 'search_path=pg_catalog, photo',
             True, False, False)
    query = Mock(return_value=ready)
    monkeypatch.setattr(db, '_query', query)
    assert db.glare_donor_runtime_ready() is True
    assert query.call_count == 2
    assert 'v28_glare_donor_source_ready' in query.call_args_list[0].args[1][0]
    assert 'v28_glare_donor_input_evidence' in query.call_args_list[1].args[1][0]
    query.return_value = (*ready[:4], True, False)
    assert db.glare_donor_runtime_ready() is False


def test_glare_donor_sql_adapter_binds_exact_attempt_and_proof(monkeypatch):
    db = TaskDatabase('unused')
    query = Mock(return_value=(True,))
    monkeypatch.setattr(db, '_query', query)
    command = SimpleNamespace(tenant_id=uuid4(), run_id=uuid4(), task_id=uuid4(),
        attempt=3, params=SimpleNamespace(artifact_id=uuid4(), sha256='a' * 64))
    lease = SimpleNamespace(command=command, fence=7, worker_id=uuid4(),
                            input_digest='b' * 64)
    assert db.glare_donor_source_ready(lease, b'{}', b'{"proof":1}') is True
    sql, args = query.call_args.args
    assert 'v28_glare_donor_source_ready' in sql
    assert args == (command.tenant_id, command.run_id, command.task_id, 3, 7,
                    lease.worker_id, command.params.artifact_id, 'a' * 64,
                    '{}', 'b' * 64, '{"proof":1}')
    query.return_value = (False,)
    assert db.glare_donor_source_ready(lease, b'{}', b'{"proof":1}') is False
    ref = SimpleNamespace(artifact_id=uuid4(), sha256='c' * 64)
    location = {'bucket': 'photos', 'object_key': 'pinned',
                'object_version': 'version-1', 'sha256': ref.sha256,
                'bytes': 128, 'media_type': 'application/octet-stream',
                'role': 'pixels', 'source': 'input'}
    query.return_value = (location,)
    assert db.glare_donor_input_evidence(lease, b'{}', b'{"proof":1}', ref) == location
    sql, args = query.call_args.args
    assert 'v28_glare_donor_input_evidence' in sql
    assert args[-3:] == ('{"proof":1', ref.artifact_id, ref.sha256)


@pytest.mark.parametrize('backend,resource_class', [
    ('mps', 'gpu'), ('cpu', 'cpu'), ('cpu', 'archive')])
def test_installation_refuses_wrong_worker(backend, resource_class):
    with pytest.raises(WorkerConfigurationError):
        installed_v28_operations(object(), object(), config(backend, resource_class),
                                glare_factory=lambda *_: object())


def test_donor_glare_registration_requires_live_authority_and_bounded_material():
    incomplete = SimpleNamespace(admit=lambda *_: True,
                                 reader=lambda *_: object(),
                                 material_scratch_root=object(),
                                 material_max_bytes=1025,
                                 alpha_mode='require_float32')
    with pytest.raises(WorkerConfigurationError):
        installed_v28_operations(object(), database(), config(),
                                glare_factory=lambda *_: incomplete)
    incomplete.material_max_bytes = 1024
    operations = installed_v28_operations(object(), database(), config(),
                                         glare_factory=lambda *_: incomplete)
    assert [(op.name, op.resource_class, op.version) for op in operations] == [
        ('portrait.glare', 'gpu', '1')]


def test_glare_generative_installation_requires_complete_pinned_capability():
    donor = SimpleNamespace(admit=lambda *_: True, reader=lambda *_: object(),
                            material_scratch_root=object(), material_max_bytes=100,
                            alpha_mode='require_float32')
    donor.admit_generative = lambda *_: True
    with pytest.raises(WorkerConfigurationError, match='Incomplete glare generative'):
        installed_v28_operations(object(), database(), config(),
                                glare_factory=lambda *_: donor)
    donor.acquire_inpaint = lambda *_: nullcontext(object())
    donor.model_lock_sha = 'A' * 64
    with pytest.raises(WorkerConfigurationError, match='Incomplete glare generative'):
        installed_v28_operations(object(), database(), config(),
                                glare_factory=lambda *_: donor)
    donor.model_lock_sha = 'a' * 64
    assert [op.name for op in installed_v28_operations(
        object(), database(), config(), glare_factory=lambda *_: donor)] == [
            'portrait.glare']


def test_reflection_registration_requires_pinned_lock_and_engine_owner():
    admission = SimpleNamespace(lock_sha='a' * 64,
                                admit=lambda *_: True,
                                acquire=lambda *_: object())
    operations = installed_v28_operations(object(), database(), config(),
                                         reflection_factory=lambda *_: admission)
    assert [(op.name, op.resource_class, op.version) for op in operations] == [
        ('image.reflection', 'gpu', '1')]
    admission.lock_sha = 'A' * 64
    with pytest.raises(WorkerConfigurationError):
        installed_v28_operations(object(), database(), config(),
                                reflection_factory=lambda *_: admission)


def test_both_handlers_keep_their_own_executor_and_admission(monkeypatch):
    from photo_worker.operations.image import reflection
    from photo_worker.operations.portrait import glare
    from photo_worker.runtime import v28_installation

    donor = SimpleNamespace(admit=lambda *_: True, reader=lambda *_: object(),
                            material_scratch_root=object(), material_max_bytes=100,
                            alpha_mode='require_float32')
    model = SimpleNamespace(lock_sha='a' * 64, admit=lambda *_: True,
                            acquire=lambda *_: nullcontext(object()))
    calls = []
    task_artifacts = SimpleNamespace(params=lambda _: object(),
                                     load_v2_index=Mock())
    monkeypatch.setattr(v28_installation.TaskArtifacts, 'create',
                        lambda *_, **__: nullcontext(task_artifacts))
    monkeypatch.setattr(v28_installation, 'canonical_bytes', lambda _: b'{}')
    monkeypatch.setattr(glare, 'execute',
                        lambda ctx, artifacts, admission: calls.append(('glare', artifacts, admission)))
    monkeypatch.setattr(reflection, 'execute',
                        lambda ctx, artifacts, admission: calls.append(('reflection', artifacts, admission)))
    operations = installed_v28_operations(object(), database(), config(),
        glare_factory=lambda *_: donor, reflection_factory=lambda *_: model)
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend': 'cpu'}),
                          check_active=lambda: None)
    for operation in operations:
        operation.execute(ctx)
    assert len(calls) == 2
    assert calls[0][0:2] == ('glare', task_artifacts)
    assert calls[0][2].provider is donor
    assert calls[1] == ('reflection', task_artifacts, model)


@pytest.mark.parametrize('operation_name', ['portrait.glare', 'image.reflection'])
@pytest.mark.parametrize('revoked_gate', ['lease', 'qualification'])
def test_v28_revoked_authority_after_handler_refuses_result(
        monkeypatch, operation_name, revoked_gate):
    from photo_worker.operations.image import reflection
    from photo_worker.operations.portrait import glare
    from photo_worker.runtime import v28_installation

    lease_gate = Mock(side_effect=[True, True, False] if revoked_gate == 'lease'
                      else [True, True, True])
    qualification_gate = Mock(side_effect=[True, True, False]
                              if revoked_gate == 'qualification' else [True, True])
    authority = SimpleNamespace(restoration_lease_ready=lease_gate,
        restoration_qualification_ready=qualification_gate,
        glare_donor_source_ready=lambda *_: True)
    donor = SimpleNamespace(admit=lambda *_: True, reader=lambda *_: object(),
        material_scratch_root=object(), material_max_bytes=100,
        alpha_mode='require_float32')
    model = SimpleNamespace(lock_sha='a' * 64, admit=lambda *_: True,
        acquire=lambda *_: nullcontext(object()))
    task_artifacts = SimpleNamespace(params=lambda _: object(),
                                     load_v2_index=Mock())
    monkeypatch.setattr(v28_installation.TaskArtifacts, 'create',
                        lambda *_, **__: nullcontext(task_artifacts))
    monkeypatch.setattr(v28_installation, 'canonical_bytes', lambda _: b'{}')
    handler = Mock(return_value=object())
    monkeypatch.setattr(glare if operation_name == 'portrait.glare' else reflection,
                        'execute', handler)
    operations = installed_v28_operations(object(), authority, config(),
        glare_factory=(lambda *_: donor) if operation_name == 'portrait.glare' else None,
        reflection_factory=(lambda *_: model) if operation_name == 'image.reflection' else None)
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend': 'cpu'}),
                          check_active=lambda: None)
    with pytest.raises(OperationFailure, match='V28_AUTHORITY_UNAVAILABLE'):
        operations[0].execute(ctx)
    handler.assert_called_once()
    assert lease_gate.call_count == 3
    assert qualification_gate.call_count == (2 if revoked_gate == 'lease' else 3)


def test_glare_admission_rechecks_server_donor_proof_each_time(monkeypatch):
    from photo_worker.runtime import v28_installation
    monkeypatch.setattr(v28_installation, 'canonical_bytes', lambda _: b'{}')
    provider = SimpleNamespace(admit=Mock(return_value=True))
    authority = Mock(return_value=True)
    admission = v28_installation._LeaseBoundGlareAdmission(
        provider, SimpleNamespace(glare_donor_source_ready=authority))
    ctx = SimpleNamespace(lease=object(), check_active=lambda: None)
    for _ in range(2):
        assert admission.admit(ctx, object(), b'proof', None, None, None) is True
    assert authority.call_count == 2
    assert provider.admit.call_count == 2
    authority.return_value = False
    with pytest.raises(OperationFailure, match='GLARE_DONOR_AUTHORITY_UNAVAILABLE'):
        admission.admit(ctx, object(), b'proof', None, None, None)
    assert provider.admit.call_count == 2


def test_live_sql_gate_refuses_execution_before_operation_handler(monkeypatch):
    from photo_worker.operations.image import reflection
    from photo_worker.runtime import v28_installation

    admission = SimpleNamespace(lock_sha='a' * 64, admit=lambda *_: True,
                                acquire=lambda *_: nullcontext(object()))
    task_artifacts = SimpleNamespace(params=lambda _: object(),
                                     load_v2_index=Mock())
    monkeypatch.setattr(v28_installation.TaskArtifacts, 'create',
                        lambda *_, **__: nullcontext(task_artifacts))
    monkeypatch.setattr(v28_installation, 'canonical_bytes', lambda _: b'{}')
    called = []
    monkeypatch.setattr(reflection, 'execute', lambda *_: called.append(True))
    operation = installed_v28_operations(object(), database(False), config(),
        reflection_factory=lambda *_: admission)[0]
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend': 'cpu'}),
                          check_active=lambda: None)
    with pytest.raises(OperationFailure, match='V28_AUTHORITY_UNAVAILABLE'):
        operation.execute(ctx)
    assert called == []


def test_live_qualification_gate_refuses_execution_before_operation_handler(monkeypatch):
    from photo_worker.operations.image import reflection
    from photo_worker.runtime import v28_installation

    admission = SimpleNamespace(lock_sha='a' * 64, admit=lambda *_: True,
                                acquire=lambda *_: nullcontext(object()))
    monkeypatch.setattr(v28_installation.TaskArtifacts, 'create',
                        lambda *_, **__: nullcontext(SimpleNamespace(
                            params=lambda _: object(), load_v2_index=Mock())))
    monkeypatch.setattr(v28_installation, 'canonical_bytes', lambda _: b'{}')
    called = []
    monkeypatch.setattr(reflection, 'execute', lambda *_: called.append(True))
    operation = installed_v28_operations(object(), database(qualified=False), config(),
        reflection_factory=lambda *_: admission)[0]
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend': 'cpu'}),
                          check_active=lambda: None)
    with pytest.raises(OperationFailure, match='V28_AUTHORITY_UNAVAILABLE'):
        operation.execute(ctx)
    assert called == []


def test_live_sql_gate_requires_database_method():
    with pytest.raises(WorkerConfigurationError, match='SQL lease/qualification gates'):
        installed_v28_operations(object(), object(), config(),
            reflection_factory=lambda *_: object())


def test_v28_index_opens_only_after_live_authority(monkeypatch):
    from photo_worker.operations.image import reflection
    from photo_worker.runtime import v28_installation

    events = []
    task_artifacts = SimpleNamespace(
        params=lambda _: events.append('params') or object(),
        load_v2_index=lambda: events.append('index'))
    def create(*_, **kwargs):
        assert kwargs == {'defer_v2_index': True}
        events.append('create')
        return nullcontext(task_artifacts)
    monkeypatch.setattr(v28_installation.TaskArtifacts, 'create', create)
    monkeypatch.setattr(v28_installation, 'canonical_bytes', lambda _: b'{}')
    monkeypatch.setattr(reflection, 'execute',
                        lambda *_: events.append('handler') or object())
    authority = SimpleNamespace(
        restoration_lease_ready=lambda *_: events.append('lease') or True,
        restoration_qualification_ready=lambda *_: events.append('quality') or True)
    model = SimpleNamespace(lock_sha='a' * 64, admit=lambda *_: True,
                            acquire=lambda *_: nullcontext(object()))
    operation = installed_v28_operations(object(), authority, config(),
        reflection_factory=lambda *_: model)[0]
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend': 'cpu'}),
                          check_active=lambda: None)
    operation.execute(ctx)
    assert events == ['create', 'params', 'lease', 'quality', 'index',
                      'lease', 'quality', 'handler', 'params', 'lease', 'quality',
                      'index', 'lease', 'quality']
    events.clear()
    authority.restoration_lease_ready = lambda *_: events.append('lease') or False
    with pytest.raises(OperationFailure, match='V28_AUTHORITY_UNAVAILABLE'):
        operation.execute(ctx)
    assert events == ['create', 'params', 'lease']
    events.clear()
    responses = iter((True, False))
    authority.restoration_lease_ready = lambda *_: events.append('lease') or next(responses)
    with pytest.raises(OperationFailure, match='V28_AUTHORITY_UNAVAILABLE'):
        operation.execute(ctx)
    assert events == ['create', 'params', 'lease', 'quality', 'index', 'lease']


def test_deferred_v2_artifacts_allow_only_params_before_index(monkeypatch):
    from photo_worker.runtime import artifacts as artifact_runtime

    params = SimpleNamespace(artifact_id=uuid4(), sha256='a' * 64)
    source = SimpleNamespace(artifact_id=uuid4(), sha256='b' * 64)
    command = SimpleNamespace(params=params, inputs=[source], task_id=uuid4())
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command,
        execution_profile={'renderer_version': 'v2.2'}), check_active=Mock())
    gateway = SimpleNamespace(open_stream=Mock())
    scratch = SimpleNamespace(cleanup=Mock())
    session = artifact_runtime.TaskArtifacts(ctx, gateway, scratch,
                                             deferred_v2_index=True)
    read = Mock(return_value=object())
    monkeypatch.setattr(artifact_runtime, 'read_contract', read)
    with session:
        assert session.params(object) is read.return_value
        with pytest.raises(ValueError, match='V2 input index is not ready'):
            session.contract(source, object)
        with pytest.raises(ValueError, match='V2 input index is not ready'):
            session.stage_contract(object(), 'document')
        assert read.call_count == 1
        index = SimpleNamespace(allows=lambda _: True, close=Mock())
        monkeypatch.setattr(artifact_runtime.V2InputIndex, 'build',
                            Mock(return_value=index))
        session.load_v2_index()
        assert session.contract(source, object) is read.return_value
    index.close.assert_called_once()
    scratch.cleanup.assert_called_once()
