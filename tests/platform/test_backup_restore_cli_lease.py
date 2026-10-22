"""Restore CLI owns the actual host lease across physical and SQL phases.

These scenarios author source coverage; no storage/service restore is performed.
"""
from contextlib import contextmanager
from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace
import fcntl
import signal
import pytest
import psycopg
import typer
from photo_worker import cli
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import backup_operator, backup_restore_operator as restore


def installation(tmp_path):
    settings = {'project': 'restore-cli-test', 'offhost_endpoint': 'offhost',
        'source_endpoint': 'source', 'offhost_bucket': 'backup',
        'public_key_file': '/unused/public', 'public_key_sha256': '1'*64}
    for field in ('postgres_target', 'minio_target', 'config_target', 'deletions_target'):
        path = tmp_path / field
        path.mkdir()
        settings[field] = str(path)
    raw = canonical_bytes({'schema_version': 'restore-operator-installation/v1',
        **{key: settings[key] for key in ('project', 'postgres_target',
            'minio_target', 'config_target', 'deletions_target')}})
    settings['operator_state_pin'] = sha256(raw).hexdigest()
    roots = []
    for field in ('postgres_target', 'minio_target', 'config_target', 'deletions_target'):
        path = Path(settings[field])
        root = path.parent / ('.photo-restore-' + sha256(str(path).encode()).hexdigest())
        root.mkdir(mode=0o700)
        manifest = root / 'installation.json'
        manifest.write_bytes(raw)
        manifest.chmod(0o600)
        roots.append(root)
    return settings, roots


def assert_locked(roots):
    for root in roots:
        with (root / 'phase.lock').open('rb') as handle:
            with pytest.raises(BlockingIOError):
                fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)


def assert_released(roots):
    for root in roots:
        with (root / 'phase.lock').open('rb') as handle:
            fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


@pytest.mark.parametrize('failure', [None, 'physical', 'bind', 'connect', 'reconcile', 'close'])
def test_restore_holds_real_lease_through_all_phases_and_connection_cleanup(tmp_path, monkeypatch, failure):
    settings, roots = installation(tmp_path)
    stages = []
    handlers = {signal.SIGINT: object(), signal.SIGTERM: object()}
    originals = dict(handlers)
    def set_signal(name, handler):
        previous = handlers[name]
        handlers[name] = handler
        return previous
    monkeypatch.setattr(signal, 'signal', set_signal)
    monkeypatch.setenv('PHOTO_RESTORE_DATABASE_URL', 'restored-dsn')
    monkeypatch.setenv('PHOTO_RESTORE_CONSENT_DATABASE_URL', 'consent-dsn')
    monkeypatch.setattr(restore, 'load_restore_operator_config', lambda path: settings)
    monkeypatch.setattr(backup_operator, 'minio_client', lambda *a: object())
    def stage(name):
        assert_locked(roots)
        stages.append(name)
        if failure == name:
            raise RuntimeError('injected ' + name)
    def checkpoint(*args):
        stage('checkpoint')
        return object()
    def physical(*args, **kwargs):
        stage('physical')
        return object()
    def bind(*args, **kwargs):
        stage('bind')
        return {'dbname': 'isolated'}
    @contextmanager
    def connect(*args, **kwargs):
        assert kwargs['connect_timeout'] == 5
        assert kwargs['autocommit'] is True
        assert kwargs['options'] == '-c statement_timeout=5000 -c lock_timeout=1000'
        stage('connect')
        try:
            yield object()
        finally:
            stage('close')
    def reconcile(*args, **kwargs):
        stage('reconcile')
        return (SimpleNamespace(preflight=SimpleNamespace(run_id='run')),
                SimpleNamespace(path='/report', sha256='2'*64))
    monkeypatch.setattr(restore, 'read_checkpoint', checkpoint)
    monkeypatch.setattr(restore, 'restore_physical_checkpoint', physical)
    monkeypatch.setattr(restore, 'bind_operator_postgres', bind)
    monkeypatch.setattr(restore, 'reconcile_checkpoint', reconcile)
    monkeypatch.setattr(psycopg, 'connect', connect)
    if failure:
        with pytest.raises(typer.Exit) as error:
            cli.backup_restore(Path('/unused/config'), 'backup-id', 'publication-version')
        assert error.value.exit_code == 1
    else:
        cli.backup_restore(Path('/unused/config'), 'backup-id', 'publication-version')
        assert stages == ['checkpoint', 'physical', 'bind', 'connect', 'connect',
                          'reconcile', 'close', 'close']
    assert_released(roots)
    assert handlers == originals


def test_partial_signal_installation_restores_already_changed_handler(monkeypatch):
    original = object()
    calls = []
    def set_signal(name, handler):
        calls.append((name, handler))
        if name == signal.SIGTERM:
            raise RuntimeError('signal installation failed')
        return original
    monkeypatch.setattr(signal, 'signal', set_signal)
    with pytest.raises(typer.Exit):
        cli.backup_restore(Path('/unused/config'), 'backup-id', 'publication-version')
    assert calls[-1] == (signal.SIGINT, original)
