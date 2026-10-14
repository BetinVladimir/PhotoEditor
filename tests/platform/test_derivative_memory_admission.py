import pytest
from photo_worker.operations.derivative.memory import derivative_ram_bytes, admit_derivative
from photo_worker.runtime.executor import OperationFailure


def test_exact_budget_and_server_formula():
    required = (256 << 20) + 192 * (400 * 200 + 200 * 100)
    assert derivative_ram_bytes(400, 200, 200, 100) == required
    assert admit_derivative({'renderer_version': 'v2.2', 'resource_estimate': {'ram_bytes': required}}, 400, 200, 200, 100) == required
    with pytest.raises(OperationFailure, match='RAM_ADMISSION_EXCEEDED'):
        admit_derivative({'resource_estimate': {'ram_bytes': required - 1}}, 400, 200, 200, 100)


@pytest.mark.parametrize('value', [True, None, 0, -1, 1.5, 2**53])
def test_invalid_budget_refused(value):
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        admit_derivative({'resource_estimate': {'ram_bytes': value}}, 10, 10, 10, 10)


def test_v22_requires_admission_and_legacy_is_bounded():
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        admit_derivative({'renderer_version': 'v2.2'}, 10, 10, 10, 10)
    admit_derivative({'backend': 'cpu'}, 10, 10, 10, 10)
    with pytest.raises(OperationFailure, match='RAM_ADMISSION_EXCEEDED'):
        admit_derivative({'backend': 'cpu'}, 10000, 10000, 10000, 10000)


@pytest.mark.parametrize('dims', [(0, 1, 1, 1), (True, 1, 1, 1), (1.5, 1, 1, 1), (20001, 10000, 1, 1)])
def test_invalid_dimensions_refused(dims):
    with pytest.raises(ValueError):
        derivative_ram_bytes(*dims)


@pytest.mark.parametrize('operation', ['resize', 'sharpen'])
def test_adapter_refuses_before_pixel_io(tmp_path, monkeypatch, operation):
    import importlib
    from photo_worker.contracts.derivative import ResizeParams, SharpenParams
    from test_derivative_adapters import Artifacts
    from test_asset_adapters import context_for
    module = importlib.import_module('photo_worker.operations.derivative.' + operation)
    artifacts = Artifacts(tmp_path, ResizeParams if operation == 'resize' else SharpenParams)
    image = artifacts.contracts[artifacts.image_ref.artifact_id]
    ctx = context_for(artifacts.image_ref, 'derivative.' + operation)
    ctx.lease.command.inputs = [artifacts.image_ref, image.pixels]
    ctx.lease.execution_profile.update(renderer_version='v2.2', resource_estimate={'ram_bytes': 1})
    def no_pixels(*args, **kwargs):
        pytest.fail('pixel IO occurred before RAM admission')
    monkeypatch.setattr(module, 'open_image', no_pixels)
    with pytest.raises(OperationFailure, match='RAM_ADMISSION_EXCEEDED'):
        module.execute(ctx, artifacts)
