"""Reject invalid DepthAnything output before transferring it to CPU."""
from types import SimpleNamespace

import pytest

from photo_worker.models.adapters import TransformersVision


class _DepthTensor:
    def __init__(self, shape, floating=True):
        self.shape = shape
        self.floating = floating
        self.detached = False

    def is_floating_point(self):
        return self.floating

    def detach(self):
        self.detached = True
        return self


@pytest.mark.parametrize('shape,floating', [
    ((1, 0, 1), True), ((1, 2049, 2048), True),
    ((2, 518, 518), True), ((518, 518), True),
    ((1, 518, 518), False),
])
def test_depth_output_refuses_bad_device_shape_before_transfer(shape, floating):
    output = _DepthTensor(shape, floating)
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        TransformersVision._bounded_depth_output(output).detach()
    assert not output.detached


def test_depth_output_accepts_normal_device_shape():
    output = _DepthTensor((1, 518, 518))
    assert TransformersVision._bounded_depth_output(output) is output


def test_depth_output_refuses_missing_model_field():
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        TransformersVision._bounded_depth_output(None)


@pytest.mark.parametrize('shape', [(2, 384), (1, 383), (1, 4096)])
def test_dino_pooler_refuses_wrong_shape_before_transfer(shape):
    output = _DepthTensor(shape)
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        TransformersVision._bounded_dino_output(
            SimpleNamespace(pooler_output=output)).detach()
    assert not output.detached


def test_dino_pooler_accepts_exact_embedding_shape():
    output = _DepthTensor((1, 384))
    assert TransformersVision._bounded_dino_output(
        SimpleNamespace(pooler_output=output)) is output


def test_dino_fallback_refuses_oversized_hidden_state_before_slicing():
    class Hidden(_DepthTensor):
        def __getitem__(self, _key):
            pytest.fail('oversized hidden state was sliced')

    output = Hidden((1, 4097, 384))
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        TransformersVision._bounded_dino_output(
            SimpleNamespace(pooler_output=None, last_hidden_state=output))
    assert not output.detached
