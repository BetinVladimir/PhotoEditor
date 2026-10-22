from contextlib import nullcontext
from hashlib import sha256
from threading import Event
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy, decode_provenance
from photo_worker.operations.provenance.installation import InstalledSigningOperator
from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles
from photo_worker.runtime.signer_deployment import SignerDeployment
from photo_worker.v2 import service, signing_adopter_installation as installation


def pinned_fixture(tmp_path):
    native = tmp_path / 'native.bin'
    native.write_bytes(b'native')
    native.chmod(0o600)
    lock = {'schema_version': 'signer-runtime-files/v1', 'files': [
        {'name': 'native', 'sha256': sha256(b'native').hexdigest(), 'size_bytes': 6}]}
    pin = sha256(canonical_bytes(lock)).hexdigest()
    files = PinnedRuntimeFiles(lock, pin, {'native': native})
    policy = canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1',
        sdk_lock_sha=pin, key_handle='key', certificate_chain_sha=sha256(b'certificate').hexdigest(),
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha=sha256(b'trust').hexdigest(), revocation_mode='off'))
    deployment = SignerDeployment(tmp_path, (policy,), 'd' * 64, b'certificate', b'trust',
        'ED25519', 'key', b'serial', b'id', 'pin', (), ())
    return deployment, files


def test_signing_adopter_builds_pinned_verification_only_operator(tmp_path, monkeypatch):
    deployment, files = pinned_fixture(tmp_path)
    calls = []
    class Delegate:
        def __init__(self, sdk, pin, trust, chain, algorithm, sign, inspect, binding,
                     services, hosts):
            assert sdk is reader_sdk and pin == files.sdk_lock_sha
            assert trust == b'trust' and chain == b'certificate'
            calls.append('delegate')
            self.sign, self.inspect, self.binding = sign, inspect, binding
        def check_installation(self, policy):
            assert self.inspect(policy, 'ED25519', b'certificate') is True
        def open_context(self, policy):
            calls.append('context')
            return nullcontext(object())
    monkeypatch.setattr(installation, 'NativeSigningDelegate', Delegate)
    monkeypatch.setattr(installation, 'CertificateBoundInstallationInspector',
                        lambda callback, **_kwargs: callback)
    reader_sdk = SimpleNamespace(
        Context=SimpleNamespace(from_dict=lambda *_args: nullcontext(object())),
        Reader=lambda *_args, **_kwargs: object())
    operator = installation.build_signing_adopter_operator(deployment, sdk=reader_sdk,
        runtime_files=files, native_authority=lambda *_args: True,
        native_inspector=lambda *_args: True, verify_binding=lambda *_args: True,
        check_active=lambda: True)
    assert isinstance(operator, InstalledSigningOperator)
    policy = decode_provenance(deployment.policies[0], SigningPolicy, 1 << 20)
    assert operator.policy_pins() == ({'id': str(policy.id),
        'sha': sha256(deployment.policies[0]).hexdigest()},)
    assert operator.check_policy_pins() is True
    assert calls == ['delegate', 'context']
    with pytest.raises(RuntimeError, match='CANNOT_SIGN'):
        operator._delegate.sign(None)
    native = tmp_path / 'native.bin'
    native.write_bytes(b'changed')
    with pytest.raises(ValueError):
        operator.check_installation(decode_provenance(deployment.policies[0], SigningPolicy, 1 << 20))
    with pytest.raises(ValueError):
        operator.check_policy_pins()


def test_deployed_signing_adopter_passes_verified_operator_and_sdk(tmp_path, monkeypatch):
    deployment, files = pinned_fixture(tmp_path)
    sdk = SimpleNamespace(Reader=lambda *_args: object())
    operator = object()
    seen = []
    monkeypatch.setattr(installation, 'load_signer_deployment', lambda *_args: deployment)
    monkeypatch.setattr(installation, 'build_signing_adopter_operator',
        lambda loaded, **kwargs: seen.append((loaded, kwargs)) or operator)
    monkeypatch.setattr(service, 'run_adopter',
        lambda *args, **kwargs: seen.append((args, kwargs)))
    stop = Event()
    config = object()
    installation.run_deployed_signing_adopter(config, stop, tmp_path,
        sdk=sdk, runtime_files=files, native_authority=lambda *_args: True,
        native_inspector=lambda *_args: True, verify_binding=lambda *_args: True)
    assert seen[0][0] is deployment
    assert seen[1] == ((config, stop), {'signing_operator': operator, 'signing_sdk': sdk})
    with pytest.raises(ValueError):
        installation.run_deployed_signing_adopter(config, Event(), tmp_path,
            sdk=sdk, runtime_files=None, native_authority=lambda *_args: True,
            native_inspector=lambda *_args: True, verify_binding=lambda *_args: True)
    assert len(seen) == 2


def test_adopter_runtime_transports_signing_operator_and_pinned_sdk(tmp_path, monkeypatch):
    deployment, _ = pinned_fixture(tmp_path)
    operator = InstalledSigningOperator(deployment.policies, lambda *_args: True, object())
    sdk = SimpleNamespace(Reader=lambda *_args: object())
    stop = Event()
    captured = []
    class Connection:
        autocommit = True
        def execute(self, query, args=()):
            assert "current_user=" in query
            return SimpleNamespace(fetchone=lambda:(True,))
        def __enter__(self):
            return self
        def __exit__(self, *_args):
            return False
    class Adopter:
        def __init__(self, _read, _write, _objects, _bucket, **options):
            captured.append(options)
        def run_once(self):
            stop.set()
    monkeypatch.setattr(service.psycopg, 'connect', lambda *_args, **_kwargs: Connection())
    monkeypatch.setattr(service, 'Minio', lambda *_args, **_kwargs: object())
    monkeypatch.setattr(service, 'Adopter', Adopter)
    from photo_worker.v2 import signing_adopter_runtime
    monkeypatch.setattr(signing_adopter_runtime, 'require_signing_adopter_runtime_schema',
                        lambda _connection: None)
    config = service.AdopterConfig('postgres://unused', 'https://example.test',
        'photo', 'key', 'secret', layer_scratch_root=tmp_path,
        layer_scratch_bytes=1 << 30)
    service.run_adopter(config, stop, signing_operator=operator, signing_sdk=sdk)
    assert len(captured) == 1
    assert captured[0]['signing_operator'] is operator
    assert captured[0]['signing_sdk'] is sdk
    with pytest.raises(ValueError, match='operator and SDK'):
        service.run_adopter(config, Event(), signing_operator=operator)


def test_signing_adopter_lease_follows_signer_health_independent_of_other_retries(tmp_path, monkeypatch):
    deployment, _ = pinned_fixture(tmp_path)
    operator = InstalledSigningOperator(deployment.policies, lambda *_args: True, object())
    sdk = SimpleNamespace(Reader=lambda *_args: object())
    events = []
    class Stop:
        stopped = False
        def is_set(self): return self.stopped
        def set(self): self.stopped = True
        def wait(self, _seconds): return self.stopped
    stop = Stop()
    class Connection:
        autocommit = True
        def execute(self, query, args=()):
            assert "current_user=" in query
            return SimpleNamespace(fetchone=lambda:(True,))
        def __enter__(self): return self
        def __exit__(self, *_args): return False
    class Adopter:
        calls = 0
        def __init__(self, *_args, **_kwargs): pass
        def run_once(self):
            self.calls += 1
            events.append('pass' + str(self.calls))
            # An unrelated import retry must not keep the signer lane dark.
            self.signing_runtime_healthy = self.calls != 3
            if self.calls == 4: stop.set()
            return {'retry': 1 if self.calls in (1, 3) else 0}
    class Lease:
        def __init__(self, _connection, _stop, _bucket, pins, qualify_pins):
            assert pins == operator.policy_pins()
            assert qualify_pins == operator.check_policy_pins
        def __enter__(self): events.append('announce');return self
        def set_ready(self, ready): events.append('ready' + str(ready))
        def __exit__(self, *_args): events.append('withdraw');return False
    from photo_worker.v2 import signing_adopter_runtime
    monkeypatch.setattr(service.psycopg, 'connect', lambda *_args, **_kwargs: Connection())
    monkeypatch.setattr(service, 'Minio', lambda *_args, **_kwargs: object())
    monkeypatch.setattr(service, 'Adopter', Adopter)
    monkeypatch.setattr(signing_adopter_runtime, 'require_signing_adopter_runtime_schema',
                        lambda _connection: None)
    monkeypatch.setattr(signing_adopter_runtime, 'SigningAdopterRuntimeLease', Lease)
    config = service.AdopterConfig('postgres://unused', 'https://example.test',
        'photo', 'key', 'secret', layer_scratch_root=tmp_path,
        layer_scratch_bytes=1 << 30)
    service.run_adopter(config, stop, signing_operator=operator, signing_sdk=sdk)
    assert events == ['pass1', 'announce', 'pass2', 'readyTrue', 'pass3', 'readyFalse',
                      'pass4', 'withdraw']
