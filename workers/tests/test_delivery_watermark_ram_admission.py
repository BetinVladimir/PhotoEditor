"""Operation-level RAM gate must run before physical source/mark access."""
from types import SimpleNamespace

import pytest

from photo_worker.operations.derivative import watermark
from photo_worker.runtime.executor import OperationFailure


class OpenReached(RuntimeError):
    pass


@pytest.mark.parametrize("mode", ["short", "missing", "invalid", "exact", "overflow"])
def test_watermark_operation_checks_ram_before_opening_arrays(monkeypatch, mode):
    source_ref, mark_ref = object(), object()
    source = SimpleNamespace(canvas=SimpleNamespace(width=1000, height=800), color="working", alpha=None)
    mark = SimpleNamespace(canvas=SimpleNamespace(width=400, height=200), color="working", alpha=object())
    params = SimpleNamespace(image=source_ref, watermark=mark_ref, max_width_fraction=.25, margin_x=0., margin_y=.99 if mode == "overflow" else 0., anchor="top_left")
    artifacts = SimpleNamespace(params=lambda contract: params)
    required = (256 << 20) + 192 * (1000 * 800 + 400 * 200)
    profile = {"backend": "cpu", "renderer_version": "v2.2"}
    if mode != "missing":
        profile["resource_estimate"] = {"ram_bytes": required if mode in {"exact", "overflow"} else required - 1 if mode == "short" else True}
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile=profile, command=SimpleNamespace(inputs=["source", "mark"])))
    events = []

    def validate(context, store, ref):
        assert context is ctx and store is artifacts
        if ref is source_ref:
            events.append("validate source")
            return source, ["source"]
        assert ref is mark_ref
        events.append("validate mark")
        return mark, ["mark"]

    def open_arrays(*args):
        events.append("open")
        raise OpenReached()

    monkeypatch.setattr(watermark, "validate_image", validate)
    monkeypatch.setattr(watermark, "open_image", open_arrays)
    if mode == "exact":
        with pytest.raises(OpenReached):
            watermark.execute(ctx, artifacts)
        assert events == ["validate source", "validate mark", "open"]
    else:
        with pytest.raises(OperationFailure) as caught:
            watermark.execute(ctx, artifacts)
        expected = "INPUT_INVALID" if mode == "overflow" else "RAM_ADMISSION_EXCEEDED" if mode == "short" else "CONFIG_INVALID"
        assert caught.value.error.code == expected
        assert events == ["validate source", "validate mark"]
