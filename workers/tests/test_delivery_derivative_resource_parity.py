"""Worker counterparts of Go/SQL delivery resource parity vectors."""
from types import SimpleNamespace

import pytest

from photo_worker.operations.derivative.memory import admit_derivative, derivative_ram_bytes
from photo_worker.operations.derivative.resize import _dimensions
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize(
    "source,long_edge,box,upscale,expected",
    [
        ((10, 5), 5, None, False, (5, 2)),
        ((10, 7), 5, None, False, (5, 4)),
        ((13, 7), None, (4, 4), False, (4, 2)),
        ((4, 3), 8, None, False, (4, 3)),
        ((4, 3), 8, None, True, (8, 6)),
        ((100, 1), 1, None, False, (1, 1)),
    ],
)
def test_resize_dimensions_match_delivery_profile_vectors(source, long_edge, box, upscale, expected):
    params = SimpleNamespace(
        long_edge=long_edge,
        width=None if box is None else box[0],
        height=None if box is None else box[1],
        allow_upscale=upscale,
    )
    actual = _dimensions(*source, params)
    assert actual == expected
    required = (256 << 20) + 192 * (source[0] * source[1] + expected[0] * expected[1])
    assert derivative_ram_bytes(*source, *actual) == required
    assert admit_derivative(
        {"renderer_version": "v2.2", "resource_estimate": {"ram_bytes": required}},
        *source, *actual,
    ) == required
    with pytest.raises(OperationFailure):
        admit_derivative(
            {"renderer_version": "v2.2", "resource_estimate": {"ram_bytes": required - 1}},
            *source, *actual,
        )


@pytest.mark.parametrize("budget", [None, True, 0, -1, 1.5, "1000000000", 2**53])
def test_v22_derivative_refuses_invalid_ram_budget(budget):
    profile = {"renderer_version": "v2.2"}
    if budget is not None:
        profile["resource_estimate"] = {"ram_bytes": budget}
    with pytest.raises(OperationFailure):
        admit_derivative(profile, 10, 7, 5, 4)


@pytest.mark.parametrize("dimensions", [(0, 1, 1, 1), (True, 1, 1, 1), (1.5, 1, 1, 1), (200_000_001, 1, 1, 1), (1, 1, 200_000_001, 1)])
def test_derivative_ram_refuses_invalid_or_oversized_dimensions(dimensions):
    with pytest.raises(ValueError):
        derivative_ram_bytes(*dimensions)


@pytest.mark.parametrize("source,mark", [((1000, 800), (400, 200)), ((10, 8), (10, 10))])
def test_watermark_ram_policy_uses_original_mark_before_downscale(source, mark):
    # Downscaling the mark must not discount buffers read before resampling.
    required = (256 << 20) + 192 * (source[0] * source[1] + mark[0] * mark[1])
    assert admit_derivative(
        {"renderer_version": "v2.2", "resource_estimate": {"ram_bytes": required}},
        *source, *mark,
    ) == required
    with pytest.raises(OperationFailure):
        admit_derivative(
            {"renderer_version": "v2.2", "resource_estimate": {"ram_bytes": required - 1}},
            *source, *mark,
        )
