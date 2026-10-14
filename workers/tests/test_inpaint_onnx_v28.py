"""The V28 inpaint_rgb interface has two exact inputs and bounded output."""

import sys
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.models.inpaint_onnx import ONNXInpaint
from photo_worker.models.install import ModelError
from photo_worker.runtime.executor import TaskCancelled


def fake_runtime(monkeypatch, *, output_name='output'):
    size = 256

    class Session:
        def __init__(self, path, providers):
            self.providers = providers

        def get_providers(self):
            return self.providers

        def get_inputs(self):
            return [SimpleNamespace(name='mask', type='tensor(float)',
                                    shape=[1, 1, size, size]),
                    SimpleNamespace(name='image', type='tensor(float)',
                                    shape=[1, 3, size, size])]

        def get_outputs(self):
            return [SimpleNamespace(name=output_name, type='tensor(float)',
                                    shape=[1, 3, size, size])]

        def run(self, names, tensors):
            assert names == ['output']
            assert tensors['image'].shape == (1, 3, size, size)
            assert tensors['mask'].shape == (1, 1, size, size)
            return [np.full((1, 3, size, size), .4, np.float32)]

    monkeypatch.setitem(sys.modules, 'onnxruntime', SimpleNamespace(
        get_available_providers=lambda: ['CPUExecutionProvider'],
        InferenceSession=Session))


def test_inpaint_onnx_validates_shape_and_mask(monkeypatch):
    fake_runtime(monkeypatch)
    engine = ONNXInpaint('/qualified/inpaint.onnx', 'cpu')
    result = engine.infer(np.full((32, 64, 3), .2, np.float32),
                          np.ones((32, 64), np.float32))
    assert result.shape == (32, 64, 3) and np.allclose(result, .4)
    with pytest.raises(ValueError, match='MODEL_INPUT_INVALID'):
        engine.infer(np.zeros((32, 64, 3), np.float32),
                     np.full((32, 64), np.nan, np.float32))
    with pytest.raises(TaskCancelled):
        engine.infer(np.zeros((32, 64, 3), np.float32),
                     np.ones((32, 64), np.float32), check_active=lambda: False)


def test_inpaint_onnx_rejects_wrong_output_and_provider(monkeypatch):
    fake_runtime(monkeypatch, output_name='rgb')
    with pytest.raises(ModelError, match='MODEL_CONTRACT_ERROR'):
        ONNXInpaint('/qualified/inpaint.onnx', 'cpu')
    fake_runtime(monkeypatch)
    with pytest.raises(ModelError, match='BACKEND_UNAVAILABLE'):
        ONNXInpaint('/qualified/inpaint.onnx', 'cuda')
