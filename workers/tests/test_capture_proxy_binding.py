"""Capture assessment accepts only geometry produced by capture.prepare."""

from types import SimpleNamespace

import pytest

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import Canvas, Rect
from photo_worker.operations.capture.adapter import validate_proxy


def _canvas(width, height, space_id='capture'):
    return Canvas(width=width, height=height, orientation=1,
                  space_id=space_id, source_sha256='a' * 64)


def _pair(proxy_canvas=None, **transform_changes):
    source_canvas = _canvas(4000, 3000)
    proxy_canvas = proxy_canvas or _canvas(2048, 1536)
    source = SimpleNamespace(canvas=source_canvas, alpha=None,
                             color=SimpleNamespace(profile_id='work_rec2020_gamma24_v1'),
                             transform_chain=SimpleNamespace(steps=[]))
    transform = SimpleNamespace(kind='matrix', from_canvas=source_canvas,
                                to_canvas=proxy_canvas, invertibility='invertible',
                                valid_region=Rect(x=0, y=0, width=proxy_canvas.width,
                                                  height=proxy_canvas.height),
                                forward_matrix=[.512, 0, -.244, 0, .512, -.244, 0, 0, 1])
    transform.__dict__.update(transform_changes)
    proxy = SimpleNamespace(canvas=proxy_canvas, alpha=None, color=source.color,
                            dtype='float32', transform_chain=SimpleNamespace(steps=[transform]))
    return source, proxy, SimpleNamespace(canvas_hash=canonical_hash(source_canvas))


def test_exact_capture_prepare_geometry_is_accepted():
    validate_proxy(*_pair())


@pytest.mark.parametrize('proxy_canvas', [
    _canvas(1024, 768), _canvas(2048, 1024), _canvas(2048, 1536, 'other')])
def test_changed_proxy_size_or_space_is_refused(proxy_canvas):
    with pytest.raises(ValueError, match='CAPTURE_PROXY_BINDING_INVALID'):
        validate_proxy(*_pair(proxy_canvas))


@pytest.mark.parametrize('changes', [
    {'invertibility': 'partial'},
    {'valid_region': Rect(x=1, y=0, width=2047, height=1536)},
    {'to_canvas': _canvas(1024, 768)},
])
def test_changed_transform_authority_is_refused(changes):
    with pytest.raises(ValueError, match='CAPTURE_PROXY_TRANSFORM_INVALID'):
        validate_proxy(*_pair(**changes))
