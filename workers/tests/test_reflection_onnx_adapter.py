"""Reflection adapter admits only the declared ONNX tensor interface."""
from types import SimpleNamespace
import sys
from pathlib import Path

import numpy as np
import pytest

from photo_worker.models.install import ModelError
from photo_worker.models.adapters import factories
from photo_worker.models.lock import MODEL_IDS
from photo_worker.models.reflection_onnx import ONNXReflection
from photo_worker.runtime.executor import TaskCancelled


def fake_runtime(monkeypatch, *, output_shape=None, output_name='output',
                 provider='CPUExecutionProvider', run_result=None, run_error=None):
    size = 128
    class Session:
        def __init__(self, _path, providers):
            self.providers = providers
        def get_providers(self):
            return self.providers
        def get_inputs(self):
            return [SimpleNamespace(name='image', type='tensor(float)', shape=[1, 3, size, size])]
        def get_outputs(self):
            return [SimpleNamespace(name=output_name, type='tensor(float)',
                                    shape=output_shape or [1, 3, size, size])]
        def run(self, names, inputs):
            assert names == ['output'] and inputs['image'].shape == (1, 3, size, size)
            if run_error is not None:
                raise run_error
            if run_result is not None:
                return run_result
            return [np.full((1, 3, size, size), .4, np.float32)]
    monkeypatch.setitem(sys.modules, 'onnxruntime', SimpleNamespace(
        get_available_providers=lambda: [provider], InferenceSession=Session))


def test_reflection_adapter_preserves_input_shape_and_bounds(monkeypatch):
    fake_runtime(monkeypatch)
    adapter = ONNXReflection('/qualified/model.onnx', 'cpu')
    result = adapter.infer(np.full((32, 64, 3), .2, np.float32))
    assert result.shape == (32, 64, 3) and result.dtype == np.float32
    assert np.allclose(result, .4)
    with pytest.raises(TaskCancelled):
        adapter.infer(np.zeros((32, 64, 3), np.float32), check_active=lambda: False)


def test_reflection_model_lock_factory_requires_exact_onnx_layout(monkeypatch):
    fake_runtime(monkeypatch)
    assert 'reflection_rgb' in MODEL_IDS
    entry = SimpleNamespace(architecture='onnx.reflection_rgb.v2_1',
                            preprocessing_id='srgb_nchw_float32_0_1')
    create = factories()['reflection_rgb']
    model = create(entry, {'model.onnx': Path('/qualified/model.onnx')}, 'cpu')
    assert isinstance(model, ONNXReflection)
    with pytest.raises(ModelError, match='MODEL_LAYOUT_INVALID'):
        create(entry, {'model.onnx': Path('/qualified/model.onnx'),
                       'other.json': Path('/qualified/other.json')}, 'cpu')
    entry.preprocessing_id = 'unknown'
    with pytest.raises(ModelError, match='MODEL_ARCHITECTURE_INVALID'):
        create(entry, {'model.onnx': Path('/qualified/model.onnx')}, 'cpu')


def test_reflection_adapter_rejects_wrong_interface_and_provider(monkeypatch):
    fake_runtime(monkeypatch, output_name='rgb')
    with pytest.raises(ModelError, match='MODEL_CONTRACT_ERROR'):
        ONNXReflection('/qualified/model.onnx', 'cpu')
    fake_runtime(monkeypatch)
    with pytest.raises(ModelError, match='BACKEND_UNAVAILABLE'):
        ONNXReflection('/qualified/model.onnx', 'cuda')


def test_reflection_adapter_rejects_untrusted_pixels_and_outputs(monkeypatch):
    fake_runtime(monkeypatch)
    adapter = ONNXReflection('/qualified/model.onnx', 'cpu')
    with pytest.raises(ValueError, match='MODEL_INPUT_INVALID'):
        adapter.infer(np.zeros((8, 9, 3), np.float64))
    with pytest.raises(ValueError, match='MODEL_INPUT_INVALID'):
        adapter.infer(np.full((8, 9, 3), np.nan, np.float32))
    fake_runtime(monkeypatch, run_result=[np.full((1, 3, 128, 128), 1.1, np.float32)])
    adapter = ONNXReflection('/qualified/model.onnx', 'cpu')
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        adapter.infer(np.zeros((8, 9, 3), np.float32))


def test_reflection_adapter_maps_session_failure(monkeypatch):
    fake_runtime(monkeypatch, run_error=RuntimeError('provider failed'))
    adapter = ONNXReflection('/qualified/model.onnx', 'cpu')
    with pytest.raises(ModelError, match='MODEL_INFERENCE_ERROR'):
        adapter.infer(np.zeros((8, 9, 3), np.float32))
