from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.models.capture_onnx import ONNXCapture


def engine(family, output):
    model = ONNXCapture.__new__(ONNXCapture)
    model.family, model.size = family, 32
    model.output_name = 'probabilities' if family == 'eye_open' else 'mask'
    calls = []
    def run(names, inputs):
        calls.append(inputs['image'])
        return [output]
    model.session = SimpleNamespace(run=run)
    return model, calls


def test_eye_probabilities_are_not_logits_or_reshaped_guesses():
    model, calls = engine('eye_open', np.array([[.1, .9]], np.float32))
    result = model.infer(np.ones((16, 32, 3), np.float32))
    assert np.allclose(result, [.1, .9])
    assert calls[0].shape == (1, 3, 32, 32)
    assert np.all(calls[0][:, :, :8] == 0)
    for output in [np.array([.1, .9], np.float32), np.array([[0., 2.]], np.float32),
                   np.array([[.1, np.nan]], np.float32), np.array([[.1, .9]], np.float64)]:
        model, _ = engine('eye_open', output)
        with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
            model.infer(np.ones((16, 32, 3), np.float32))


def test_glare_mask_undoes_letterbox_without_counting_padding():
    output = np.zeros((1, 1, 32, 32), np.float32)
    output[:, :, 8:24] = .75
    model, _ = engine('glare_mask', output)
    mask = model.infer(np.ones((16, 32, 3), np.float32))
    assert mask.shape == (16, 32)
    assert np.all(mask == .75)
