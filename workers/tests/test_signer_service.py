"""Dedicated process composition tests; no broker/DB/native qualification."""
from types import SimpleNamespace
from pathlib import Path
from uuid import uuid4

import pytest

from photo_worker.runtime.executor import Executor, Operation
from photo_worker.runtime.service import WorkerConfigurationError
from photo_worker.runtime import signer_service


def config():
    return SimpleNamespace(capacity_policy=None, resource_class='cpu', backend='cpu', delivery_enabled=False,
        interchange_enabled=False, ca=Path('/fixture/ca'), certificate=Path('/fixture/cert'),
        private_key=Path('/fixture/key'), scratch_root=Path('/fixture/scratch'),
        gateway_origin='https://gateway.example.com', max_scratch_bytes=1024,
        max_download_bytes=1024, max_upload_bytes=1024, database_url='fixture',
        worker_id=uuid4(), amqp_url='amqps://broker.example.com', development=False)


def test_signer_transport_is_targeted_and_only_signs(monkeypatch):
    candidate = config()
    db = SimpleNamespace(runtime_operations=lambda *args: frozenset({'provenance.sign'}))
    monkeypatch.setattr(signer_service, 'TaskDatabase', lambda _: db)
    monkeypatch.setattr(signer_service, 'client_tls', lambda *args: object())
    def factory(database, worker_id, artifacts):
        assert database is db and worker_id == candidate.worker_id
        assert artifacts.origin == candidate.gateway_origin
        return Executor(db, worker_id, [Operation('provenance.sign','cpu',lambda ctx:None)])
    database, transport = signer_service.build_signer_worker(candidate, factory)
    assert database is db
    assert transport.queue == f'q.worker.{candidate.worker_id}.cpu'
    assert set(transport.executor.operations) == {'provenance.sign'}


@pytest.mark.parametrize('field,value', [('resource_class', 'gpu'), ('backend', 'mps'),
    ('delivery_enabled', True), ('interchange_enabled', True),
    ('icc_root', Path('/fixture/icc')), ('model_root', Path('/fixture/models')), ('layer_runtime_sha', 'a'*64)])
def test_mixed_installations_refused_before_io(monkeypatch, field, value):
    candidate = config()
    setattr(candidate, field, value)
    monkeypatch.setattr(signer_service, 'client_tls', lambda *args: pytest.fail('TLS reached'))
    with pytest.raises(WorkerConfigurationError):
        signer_service.build_signer_worker(candidate, lambda *args: pytest.fail('factory reached'))


@pytest.mark.parametrize('mode', ['operation', 'worker', 'database'])
def test_wrong_executor_identity_or_operations_refused(monkeypatch, mode):
    candidate, db = config(), SimpleNamespace(runtime_operations=lambda *args: frozenset({'provenance.sign'}))
    monkeypatch.setattr(signer_service, 'TaskDatabase', lambda _: db)
    monkeypatch.setattr(signer_service, 'client_tls', lambda *args: object())
    def factory(database, worker_id, artifacts):
        return Executor(object() if mode == 'database' else database,
            uuid4() if mode == 'worker' else worker_id,
            [Operation('io.thumbnail' if mode == 'operation' else 'provenance.sign','cpu',lambda ctx:None)])
    with pytest.raises(WorkerConfigurationError):
        signer_service.build_signer_worker(candidate, factory)


def test_signer_runs_shared_lifecycle(monkeypatch):
    candidate, factory, db, transport = config(), object(), object(), object()
    calls = []
    def build(conf, install):
        assert conf is candidate and install is factory
        return db, transport
    monkeypatch.setattr(signer_service, 'build_signer_worker', build)
    monkeypatch.setattr(signer_service, 'run_built_worker', lambda *args: calls.append(args))
    signer_service.run_signer_worker(candidate, factory)
    assert calls == [(candidate, db, transport)]


def test_deployed_signer_requires_runtime_authority_before_loading(monkeypatch):
    import photo_worker.runtime.signer_deployment as deployment_module
    monkeypatch.setattr(deployment_module, 'load_signer_deployment', lambda *a: pytest.fail('deployment loaded'))
    with pytest.raises(WorkerConfigurationError, match='pinned native deployment authority'):
        signer_service.run_deployed_signer(config(), '/fixture', sdk=object(), pkcs11=object(),
            library=object(), native_authority=lambda *a: True, native_inspector=lambda *a: True,
            verify_binding=lambda *a: True, runtime_files=None)


def test_deployed_signer_refuses_explicit_false_before_deployment_io(tmp_path, monkeypatch):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles
    import photo_worker.runtime.signer_deployment as deployment_module
    native = tmp_path/'native.bin'
    native.write_bytes(b'native')
    native.chmod(0o600)
    lock = {'schema_version': 'signer-runtime-files/v1', 'files': [
        {'name': 'native', 'sha256': sha256(b'native').hexdigest(), 'size_bytes': 6}]}
    files = PinnedRuntimeFiles(lock, sha256(canonical_bytes(lock)).hexdigest(), {'native': native})
    monkeypatch.setattr(deployment_module, 'load_signer_deployment',
        lambda *args: pytest.fail('deployment read after cancellation'))
    with pytest.raises(RuntimeError, match='SIGNER_TASK_INACTIVE'):
        signer_service.run_deployed_signer(config(), tmp_path.resolve(), sdk=object(),
            pkcs11=object(), library=object(), native_authority=lambda *args: True,
            native_inspector=lambda *args: True, verify_binding=lambda *args: True,
            runtime_files=files, check_active=lambda: False)


@pytest.mark.parametrize('worker_failure',[False,True])
def test_deployed_signer_composes_manifest_with_lifecycle(tmp_path, monkeypatch, worker_failure):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles
    import photo_worker.runtime.signer_deployment as deployment_module
    root = tmp_path.resolve()
    native = root/'native.bin'
    native.write_bytes(b'native')
    native.chmod(0o600)
    lock = {'schema_version':'signer-runtime-files/v1','files':[
        {'name':'native','sha256':sha256(b'native').hexdigest(),'size_bytes':6}]}
    sdk_sha = sha256(canonical_bytes(lock)).hexdigest()
    files = PinnedRuntimeFiles(lock,sdk_sha,{'native':native})
    from uuid import uuid4
    raw = canonical_bytes(dict(schema_version=1,id=str(uuid4()),revision='1',sdk_lock_sha=sdk_sha,
        key_handle='key',certificate_chain_sha='b'*64,timestamp_mode='off',timestamp_service_id=None,
        allowed_mime=['image/png'],trust_bundle_sha='c'*64,revocation_mode='off'))
    expected, calls, captured = object(), [], {}
    allowed, stop_inside = True, False
    def active():
        if not allowed:
            raise RuntimeError('signer stopped during native call')
    def stopping_native(*args):
        nonlocal allowed
        if stop_inside:
            allowed = False
        return True
    def build(database, worker, artifacts, **kwargs):
        calls.append((database,worker,artifacts))
        assert kwargs['runtime_files'] is files
        captured.update(kwargs)
        return expected
    deployment = SimpleNamespace(policies=(raw,),build_executor=build)
    monkeypatch.setattr(deployment_module,'load_signer_deployment',lambda *a:deployment)
    candidate = config()
    def lifecycle(conf,factory):
        nonlocal allowed, stop_inside
        assert conf is candidate
        assert factory('database',candidate.worker_id,'artifacts') is expected
        assert captured['native_inspector'](None) is True
        assert captured['verify_binding'](None) is True
        for name in ('native_inspector', 'verify_binding'):
            # Each process-owned callback must recheck the process fence after
            # returning from native code, even when it reports success.
            stop_inside = True
            with pytest.raises(RuntimeError, match='signer stopped during native call'):
                captured[name](None)
            stop_inside, allowed = False, True
        if worker_failure:raise ValueError('worker failed')
    monkeypatch.setattr(signer_service,'run_signer_worker',lifecycle)
    def invoke():return signer_service.run_deployed_signer(candidate,root,sdk=object(),pkcs11=object(),library=object(),
        native_authority=lambda *a:True,native_inspector=stopping_native,
        verify_binding=stopping_native,runtime_files=files,check_active=active)
    if worker_failure:
        with pytest.raises(ValueError,match='worker failed'):invoke()
    else:invoke()
    assert calls == [('database',candidate.worker_id,'artifacts')]
    with pytest.raises(RuntimeError,match='SIGNER_TASK_SCOPE_CLOSED'):
        captured['check_active']()
    with pytest.raises(RuntimeError,match='SIGNER_TASK_SCOPE_CLOSED'):
        captured['native_authority'](None,None,None)
    with pytest.raises(RuntimeError,match='SIGNER_TASK_SCOPE_CLOSED'):
        captured['native_inspector'](None)
    with pytest.raises(RuntimeError,match='SIGNER_TASK_SCOPE_CLOSED'):
        captured['verify_binding'](None)


@pytest.mark.parametrize('mode', ['valid', 'worker_failure', 'listener_failure', 'missing_operator'])
def test_deployed_listener_shares_operator_and_retains_stop_guard(tmp_path, monkeypatch, mode):
    from contextlib import contextmanager
    from hashlib import sha256
    import ssl
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.provenance.installation import InstalledSigningOperator
    from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles
    from photo_worker.runtime.signer_challenge_lifecycle import SigningChallengeListenerInstallation
    import photo_worker.runtime.signer_deployment as deployment_module
    native = tmp_path/'native.bin'
    native.write_bytes(b'native');native.chmod(0o600)
    lock = {'schema_version':'signer-runtime-files/v1','files':[
        {'name':'native','sha256':sha256(b'native').hexdigest(),'size_bytes':6}]}
    pin = sha256(canonical_bytes(lock)).hexdigest()
    files = PinnedRuntimeFiles(lock, pin, {'native':native})
    raw = canonical_bytes(dict(schema_version=1,id=str(uuid4()),revision='1',sdk_lock_sha=pin,
        key_handle='key',certificate_chain_sha='b'*64,timestamp_mode='off',timestamp_service_id=None,
        allowed_mime=['image/png'],trust_bundle_sha='c'*64,revocation_mode='off'))
    operator = InstalledSigningOperator([raw], lambda *args: True, object())
    captured, events = {}, []
    def build(*args, **kwargs):
        captured.update(kwargs)
        if mode != 'missing_operator':kwargs['on_operator'](operator)
        return object()
    deployment = SimpleNamespace(policies=(raw,), build_executor=build)
    monkeypatch.setattr(deployment_module, 'load_signer_deployment', lambda *args: deployment)
    candidate = config()
    def build_worker(conf, factory):
        factory(object(), candidate.worker_id, object())
        return 'database', 'transport'
    monkeypatch.setattr(signer_service, 'build_signer_worker', build_worker)
    stopped = False
    @contextmanager
    def running(self, policies, installed, *, check_active):
        nonlocal stopped
        assert policies == (raw,) and installed is operator
        events.append('listener')
        def guard():
            check_active()
            if stopped:raise RuntimeError('listener stopped')
        try:yield guard
        finally:stopped = True;events.append('close')
    monkeypatch.setattr(SigningChallengeListenerInstallation, 'running', running)
    def run(*args):
        nonlocal stopped
        events.append('worker')
        assert captured['native_authority'](None, None, None) is True
        if mode == 'worker_failure':raise ValueError('worker failed')
        if mode == 'listener_failure':stopped = True
        captured['check_active']()
    monkeypatch.setattr(signer_service, 'run_built_worker', run)
    tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    tls.minimum_version = ssl.TLSVersion.TLSv1_3;tls.verify_mode = ssl.CERT_REQUIRED
    listener = SigningChallengeListenerInstallation(('127.0.0.1', 0), tls, ('a'*64,))
    def invoke():
        signer_service.run_deployed_signer(candidate, tmp_path.resolve(), sdk=object(), pkcs11=object(),
            library=object(), native_authority=lambda *args: True, native_inspector=lambda *args: True,
            verify_binding=lambda *args: True, runtime_files=files, challenge_listener=listener)
    if mode == 'valid':invoke()
    else:
        with pytest.raises((ValueError, RuntimeError, WorkerConfigurationError)):invoke()
    if mode == 'missing_operator':assert events == []
    else:
        assert events == ['listener', 'worker', 'close']
        with pytest.raises(RuntimeError, match='SIGNER_TASK_SCOPE_CLOSED'):captured['check_active']()
        with pytest.raises(RuntimeError, match='SIGNER_TASK_SCOPE_CLOSED'):captured['native_authority'](None, None, None)

@pytest.mark.parametrize('mode', ['missing', 'mixed', 'changed'])
def test_signer_registration_fenced_before_and_after_factory(monkeypatch, mode):
    candidate = config()
    operations = frozenset() if mode == 'missing' else frozenset({'provenance.sign', 'io.thumbnail'}) if mode == 'mixed' else frozenset({'provenance.sign'})
    reads, builds = [], []
    def registered(worker, resource):
        assert worker == candidate.worker_id and resource == 'cpu'
        reads.append(True)
        if mode == 'changed' and len(reads) > 1:return frozenset({'io.thumbnail'})
        return operations
    db = SimpleNamespace(runtime_operations=registered)
    monkeypatch.setattr(signer_service, 'TaskDatabase', lambda _: db)
    monkeypatch.setattr(signer_service, 'client_tls', lambda *args: object())
    def factory(database, worker_id, artifacts):
        builds.append(True)
        return Executor(database, worker_id, [Operation('provenance.sign','cpu',lambda ctx:None)])
    with pytest.raises(WorkerConfigurationError, match='registration'):
        signer_service.build_signer_worker(candidate, factory)
    assert len(builds) == (1 if mode == 'changed' else 0)


def test_signer_build_and_lifecycle_share_policy_thread_budget(monkeypatch):
    from contextlib import contextmanager
    from photo_worker.runtime.capacity_policy import load_capacity_policy
    import photo_worker.runtime.thread_budget as budgets
    candidate = config()
    candidate.capacity_policy = load_capacity_policy(Path(__file__).resolve().parents[2]/'configs/capacity-metal-verification.json')
    events = []
    @contextmanager
    def budget(threads):
        assert threads == candidate.capacity_policy.cpu_threads_per_task
        events.append('enter')
        try: yield threads
        finally: events.append('exit')
    def build(*args):
        assert events == ['enter']
        events.append('build')
        return object(), object()
    def run(*args):
        assert events == ['enter','build']
        events.append('run')
        raise RuntimeError('fixture shutdown')
    monkeypatch.setattr(budgets, 'worker_thread_budget', budget)
    monkeypatch.setattr(signer_service, 'build_signer_worker', build)
    monkeypatch.setattr(signer_service, 'run_built_worker', run)
    with pytest.raises(RuntimeError, match='fixture shutdown'):
        signer_service.run_signer_worker(candidate, object())
    assert events == ['enter','build','run','exit']


def test_deployed_runtime_file_admission_keeps_caller_cancellation(tmp_path, monkeypatch):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.provenance.cancellation import check_signing_active
    from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles
    import photo_worker.runtime.signer_deployment as deployment_module

    native = tmp_path / 'native.bin'
    native.write_bytes(b'native')
    native.chmod(0o600)
    lock = {'schema_version': 'signer-runtime-files/v1', 'files': [
        {'name': 'native', 'sha256': sha256(b'native').hexdigest(), 'size_bytes': 6}]}
    pin = sha256(canonical_bytes(lock)).hexdigest()
    cancelled = False
    visited = []
    class CancellingFiles(PinnedRuntimeFiles):
        def check(self):
            nonlocal cancelled
            visited.append('runtime-files')
            cancelled = True
            # Simulate a file-read boundary discovering startup cancellation.
            check_signing_active()
            return super().check()
    files = CancellingFiles(lock, pin, {'native': native})
    raw = canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1', sdk_lock_sha=pin,
        key_handle='key', certificate_chain_sha='b'*64, timestamp_mode='off', timestamp_service_id=None,
        allowed_mime=['image/png'], trust_bundle_sha='c'*64, revocation_mode='off'))
    monkeypatch.setattr(deployment_module, 'load_signer_deployment',
        lambda *args: SimpleNamespace(policies=(raw,)))
    monkeypatch.setattr(signer_service, 'run_signer_worker',
        lambda *args: pytest.fail('cancelled admission reached worker lifecycle'))
    def active():
        if cancelled:
            raise RuntimeError('startup cancelled')
    with pytest.raises(RuntimeError, match='startup cancelled'):
        signer_service.run_deployed_signer(config(), tmp_path.resolve(), sdk=object(), pkcs11=object(),
            library=object(), native_authority=lambda *args: True, native_inspector=lambda *args: True,
            verify_binding=lambda *args: True, runtime_files=files, check_active=active)
    assert visited == ['runtime-files']
    # The startup context is restored on refusal; later unrelated calls do not
    # inherit the cancelled guard.
    check_signing_active()
