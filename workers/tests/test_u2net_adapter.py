import sys
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.models.adapters import ONNXU2Net
from photo_worker.models.install import ModelError


class Session:
    def __init__(self, inputs=1, outputs=1):
        self.inputs = [SimpleNamespace(name=f'input-{n}') for n in range(inputs)]
        self.outputs = [SimpleNamespace(name=f'output-{n}') for n in range(outputs)]
        self.calls = []

    def get_inputs(self):
        return self.inputs

    def get_outputs(self):
        return self.outputs

    def run(self, names, inputs):
        self.calls.append((names, inputs))
        return [np.zeros((1, 1, 320, 320), dtype=np.float32)]


def install(monkeypatch, session):
    monkeypatch.setitem(sys.modules, 'onnxruntime', SimpleNamespace(
        InferenceSession=lambda path, providers: session))


def test_u2net_constructor_retains_session_ports_for_inference(monkeypatch):
    session = Session()
    install(monkeypatch, session)
    adapter = ONNXU2Net('qualified.onnx', 'cpu')
    output = adapter.infer(np.zeros((320, 320, 3), dtype=np.float32))
    assert output.shape == (320, 320)
    assert session.calls[0][0] == ['output-0']
    assert session.calls[0][1]['input-0'].shape == (1, 3, 320, 320)


@pytest.mark.parametrize('inputs,outputs', [(0, 1), (2, 1), (1, 0)])
def test_u2net_rejects_incompatible_port_inventory(monkeypatch, inputs, outputs):
    install(monkeypatch, Session(inputs, outputs))
    with pytest.raises(ModelError, match='MODEL_ARCHITECTURE_INVALID'):
        ONNXU2Net('qualified.onnx', 'cpu')
