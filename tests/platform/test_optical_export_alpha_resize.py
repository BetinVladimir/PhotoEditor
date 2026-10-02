import numpy as np
import pytest
from photo_worker.operations.export.encode import _resize,_resize_rgba


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
