from types import SimpleNamespace

import pytest

from photo_worker.operations.derivative.watermark import _placement


@pytest.mark.parametrize("anchor,expected", [
    ("top_left", (2, 2, 0, 0)),
    ("top_right", (2, 2, 8, 0)),
    ("bottom_left", (2, 2, 0, 6)),
    ("bottom_right", (2, 2, 8, 6)),
    ("center", (2, 2, 4, 3)),
])
def test_watermark_ties_even_and_anchors(anchor, expected):
    params = SimpleNamespace(max_width_fraction=.25, margin_x=0., margin_y=0., anchor=anchor)
    assert _placement(10, 8, 10, 10, params) == expected


@pytest.mark.parametrize("anchor", ["top_left", "top_right", "bottom_left", "bottom_right", "center"])
def test_tall_mark_is_refused_before_resampling(anchor):
    params = SimpleNamespace(max_width_fraction=.25, margin_x=0., margin_y=0., anchor=anchor)
    with pytest.raises(ValueError):
        _placement(10, 1, 10, 10, params)
