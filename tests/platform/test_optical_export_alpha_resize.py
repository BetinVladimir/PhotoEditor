import numpy as np
import pytest
from photo_worker.operations.export.encode import _resize,_resize_rgba,_resize_working_image
from types import SimpleNamespace


@pytest.mark.parametrize('encoding',['gamma24','srgb'])
def test_hidden_transparent_color_does_not_bleed_into_resize(encoding):
    rgb=np.array([[[.8,0,0],[0,0,99]]],np.float32)
    alpha=np.array([[1,0]],np.float32)
    original=rgb.copy()
    output,opacity=_resize_rgba(rgb,alpha,1,1,encoding=encoding)
    np.testing.assert_allclose(output,[[[.8,0,0]]],atol=1e-6,rtol=0)
    np.testing.assert_array_equal(opacity,[[.5]])
    np.testing.assert_array_equal(rgb,original)


def test_opaque_and_unchanged_geometry_preserve_previous_values():
    rgb=np.random.default_rng(1).random((4,6,3),dtype=np.float32)
    opaque=np.ones((4,6),np.float32)
    output,alpha=_resize_rgba(rgb,opaque,3,2)
    np.testing.assert_array_equal(output,_resize(rgb,3,2))
    opaque[0,0]=0
    output,alpha=_resize_rgba(rgb,opaque,6,4)
    np.testing.assert_array_equal(output,rgb)
    np.testing.assert_array_equal(alpha,opaque)


def test_export_resize_uses_working_image_transfer_function():
    rgb=np.array([[[.2,.1,.8],[.8,.5,.1]]],np.float32)
    alpha=np.array([[1,.25]],np.float32)
    linear=SimpleNamespace(color=SimpleNamespace(encoding='linear'))
    gamma=SimpleNamespace(color=SimpleNamespace(encoding='gamma24'))
    linear_rgb,linear_alpha=_resize_working_image(rgb,alpha,1,1,linear)
    gamma_rgb,gamma_alpha=_resize_working_image(rgb,alpha,1,1,gamma)
    expected_rgb,expected_alpha=_resize_rgba(rgb,alpha,1,1,encoding='linear')
    np.testing.assert_array_equal(linear_rgb,expected_rgb)
    np.testing.assert_array_equal(linear_alpha,expected_alpha)
    np.testing.assert_array_equal(gamma_alpha,linear_alpha)
    assert not np.allclose(gamma_rgb,linear_rgb)
