"""CLI response/error contract; no installation or native SDK invocation."""
import json

from typer.testing import CliRunner

from photo_worker.cli import app
from photo_worker.runtime import signer_wheel


def test_sdk_wheel_cli_emits_receipt_without_qualification(tmp_path, monkeypatch):
    seen = []
    def verify(path):
        seen.append(path)
        return signer_wheel.SignerSDKWheelReceipt('signer-sdk-wheel/v1', 'c2pa-python',
            '0.32.4', 'manylinux_2_28_x86_64', signer_wheel.SIGNER_SDK_WHEEL_SHA256, 123)
    monkeypatch.setattr(signer_wheel, 'verify_signer_sdk_wheel', verify)
    path = tmp_path.resolve()/'sdk.whl'
    result = CliRunner().invoke(app, ['signer-verify-sdk-wheel', str(path)])
    assert result.exit_code == 0
    value = json.loads(result.stdout)
    assert value['sha256'] == signer_wheel.SIGNER_SDK_WHEEL_SHA256
    assert value['qualification'] == 'not_measured'
    assert seen == [path]


def test_sdk_wheel_cli_hides_dependency_error_details(tmp_path, monkeypatch):
    def verify(path):
        raise OSError('secret dependency detail must not appear')
    monkeypatch.setattr(signer_wheel, 'verify_signer_sdk_wheel', verify)
    result = CliRunner().invoke(app, ['signer-verify-sdk-wheel', str(tmp_path.resolve()/'sdk.whl')])
    assert result.exit_code == 2
    assert 'secret dependency detail' not in result.output
    assert 'Signer SDK wheel rejected' in result.output
