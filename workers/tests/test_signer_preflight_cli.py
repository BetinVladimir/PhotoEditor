"""Operator CLI contract; no native signer or PIN is opened."""
import json
import pytest

from typer.testing import CliRunner

from photo_worker.cli import app
from photo_worker.runtime import signer_preflight


def test_signer_file_preflight_boolean_denial_prevents_deployment_reads(tmp_path, monkeypatch):
    monkeypatch.setattr(signer_preflight,'load_signer_deployment',
        lambda *args,**kwargs:pytest.fail('deployment read after cancellation'))
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        signer_preflight.verify_signer_deployment_files(tmp_path.resolve(),
            'runtime-lock.json',{'native':str(tmp_path/'native.so')},
            check_active=lambda:False)


def test_signer_deployment_cli_uses_explicit_file_mapping(tmp_path, monkeypatch):
    seen = []
    def verify(root, lock, files):
        seen.append((root, lock, files))
        return {'valid':True,'sdk_lock_sha':'a'*64,'native_qualified':False}
    monkeypatch.setattr(signer_preflight, 'verify_signer_deployment_files', verify)
    root = tmp_path.resolve()
    native = root/'native.so'
    result = CliRunner().invoke(app, ['signer-verify-deployment', str(root),
        '--runtime-file', 'native='+str(native)])
    assert result.exit_code == 0
    assert json.loads(result.stdout)['native_qualified'] is False
    assert seen == [(root, 'runtime-lock.json', {'native':str(native)})]


def test_signer_deployment_cli_refuses_duplicate_mapping_and_hides_error(tmp_path, monkeypatch):
    root = tmp_path.resolve()
    duplicate = ['signer-verify-deployment', str(root), '--runtime-file',
        'native=/a', '--runtime-file', 'native=/b']
    result = CliRunner().invoke(app, duplicate)
    assert result.exit_code == 2
    def fail(*args):
        raise OSError('private signer credential path')
    monkeypatch.setattr(signer_preflight, 'verify_signer_deployment_files', fail)
    result = CliRunner().invoke(app, ['signer-verify-deployment', str(root),
        '--runtime-file', 'native=/a'])
    assert result.exit_code == 2
    assert 'private signer credential path' not in result.output
